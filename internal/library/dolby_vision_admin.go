package library

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// AdminDolbyVisionVerification exposes the indexed verification state, not
// stream proofs, executable identity, source paths, or permission to render.
type AdminDolbyVisionVerification struct {
	StreamIndex int
	State       string
	VerifiedAt  *time.Time `json:",omitempty"`
}

func (s *Store) readAdminDolbyVisionVerification(ctx context.Context, tx pgx.Tx, value AdminMediaInfo) ([]AdminDolbyVisionVerification, error) {
	var video []int
	for _, stream := range value.Streams {
		if stream.Type == "video" && !stream.IsExternal && !stream.IsAttachedPicture && stream.VideoRange == "DOVI" {
			video = append(video, stream.Index)
		}
	}
	if len(video) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT wanted.idx,COALESCE(stream.value->>'Codec',''),stream.value->'DolbyVision',
		q.item_id IS NOT NULL,COALESCE(q.attempts,0),q.evidence,q.verified_at
		FROM items i CROSS JOIN LATERAL unnest($2::integer[]) WITH ORDINALITY wanted(idx,pos)
		JOIN LATERAL (SELECT entry.value,wanted.pos AS position FROM jsonb_array_elements(
			CASE WHEN jsonb_typeof(i.media->'Streams')='array' THEN i.media->'Streams' ELSE '[]'::jsonb END
		) entry(value) WHERE entry.value->>'Index'=wanted.idx::text AND entry.value->>'CodecType'='video'
			AND entry.value->'IsExternal' IS DISTINCT FROM 'true'::jsonb
			AND entry.value->'IsAttachedPicture' IS DISTINCT FROM 'true'::jsonb LIMIT 1) stream ON true
		LEFT JOIN dolby_vision_verifications q ON q.item_id=i.id AND q.stream_index=wanted.idx
			AND q.source_revision=`+MediaOperationSourceRevisionSQL+` AND q.verifier_version=$3
			AND $4<>'' AND (q.evidence IS NULL OR q.tool_identity=$4)
		WHERE i.id=$1 ORDER BY stream.position`, value.ID, video, media.DolbyVisionVerificationVersion, s.dolbyVisionVerifier.identity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AdminDolbyVisionVerification
	for rows.Next() {
		var status AdminDolbyVisionVerification
		var codec string
		var declaration, proof []byte
		var queued bool
		var attempts int
		var verifiedAt *time.Time
		if err := rows.Scan(&status.StreamIndex, &codec, &declaration, &queued, &attempts, &proof, &verifiedAt); err != nil {
			return nil, err
		}
		var dv *media.DolbyVisionMetadata
		if declaration != nil {
			_ = json.Unmarshal(declaration, &dv)
		}
		status.State = "unknown"
		switch {
		case !media.DolbyVisionRPUVerificationSupported(media.Stream{CodecType: "video", Codec: codec, DolbyVision: dv}) ||
			dv.RPUValidationReason == "unsupported_configuration":
			status.State = "unsupported"
		case dv.RPUVerified:
			status.State = "verified"
		case queued && proof == nil:
			status.State = "pending"
			if attempts > 0 {
				status.State = "retrying"
			}
		case queued:
			var evidence media.DolbyVisionRPUEvidence
			if json.Unmarshal(proof, &evidence) == nil && evidence.StreamIndex == status.StreamIndex && evidence.Valid() {
				switch {
				case evidence.Verified && verifiedAt != nil:
					status.State = "verified"
					stamp := verifiedAt.UTC()
					status.VerifiedAt = &stamp
				case evidence.Reason == "unsupported_configuration":
					status.State = "unsupported"
				default:
					status.State = "failed"
				}
			}
		case dv.RPUValidationReason != "":
			status.State = "failed"
		}
		result = append(result, status)
	}
	return result, rows.Err()
}
