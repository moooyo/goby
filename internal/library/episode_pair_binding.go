package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

const episodePairBindingField = "_EpisodePairBinding"

// A root remap or reapproval can commit before another scan. Old descriptive
// facts remain inspectable, but they cannot authorize the new binding. This
// check only visits bound pairs in the existing content-policy ancestor walk.
const staleEpisodePairPolicySQL = `EXISTS (SELECT 1 FROM policy_ancestors ancestor
	JOIN items member ON member.id=ancestor.id AND member.type='Episode'
	WHERE member.local_metadata ? '` + episodePairBindingField + `'
	AND NOT EXISTS (SELECT 1 FROM library_roots root JOIN items season ON season.id=member.parent_id
		AND season.root_id=root.id AND season.library_id=root.library_id AND season.type='Season' AND season.is_folder
		JOIN items series ON series.id=season.parent_id AND series.root_id=root.id AND series.library_id=root.library_id
		AND series.type='Series' AND series.is_folder
		WHERE root.id=member.root_id AND root.library_id=member.library_id
		AND member.local_metadata->'` + episodePairBindingField + `' @> jsonb_build_object(
			'RootID',root.id,'RootRevision',root.binding_revision,'Path',member.path,
			'SeasonID',season.id,'SeriesID',series.id,'SeriesPath',series.relative_path,
			'Numbers',jsonb_build_object('Season',season.index_number),
			'File',jsonb_build_object('Identity',member.file_identity,'Size',member.file_size))))`

// episodePairBinding is private local source provenance. A retained merged NFO
// describes these bytes in this season, not every future source for the item ID.
type episodePairBinding struct {
	Numbers                                      metadata.EpisodePair
	RootID, Path, SeasonID, SeriesID, SeriesPath string
	RootIdentity, DirectoryIdentity              string
	RootRevision                                 int64
	File                                         episodePairFile
}

func (binding *episodePairBinding) same(other *episodePairBinding) bool {
	if binding == nil || other == nil {
		return binding == other
	}
	return *binding == *other
}

type episodePairFile struct {
	Identity                   string
	Size, ModifiedNS, ChangeNS int64
}

func episodePairFileFromInfo(info os.FileInfo) episodePairFile {
	return episodePairFile{Identity: fileIdentity(info), Size: info.Size(),
		ModifiedNS: catalogModifiedTime(info).UnixNano(), ChangeNS: media.FileChangeTime(info)}
}

type episodePairSource struct {
	row     rootBindingRow
	path    string
	binding episodePairBinding
}

func newEpisodePairSource(row rootBindingRow, path, parentID, rootIdentity string, request scanMetadataImport, file episodePairFile) episodePairSource {
	numbers, ok := episodeFilePair(path)
	season, physical := tvSeasonNumber(filepath.Base(filepath.Dir(path)))
	if !ok || !physical || season != numbers.Season || parentID == "" || !request.parentDefined ||
		request.index != numbers.First || request.parentIndex != numbers.Season || !validImageScanPath(path) ||
		rootIdentity == "" || request.directoryIdentity == "" || file.Identity == "" {
		return episodePairSource{}
	}
	return episodePairSource{row: row, path: path, binding: episodePairBinding{
		Numbers: numbers, RootID: row.root.id, RootRevision: row.revision, Path: filepath.Join(row.root.path, path),
		SeasonID: parentID, RootIdentity: rootIdentity, DirectoryIdentity: request.directoryIdentity, File: file,
	}}
}

// A syntactically paired file in a virtual season or below the actual season
// directory still has a display range; its sidecar cannot authorize a merge.
func (source episodePairSource) readBinding(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, lock bool) (*episodePairBinding, error) {
	binding := source.binding
	if binding.SeasonID == "" {
		return nil, nil
	}
	root := source.row.root
	directory := filepath.Dir(source.path)
	query := `SELECT series.id, series.relative_path, series.path, COALESCE(series.parent_id,'')
		FROM items season JOIN items series ON series.id=season.parent_id AND series.library_id=season.library_id AND series.root_id=season.root_id
		JOIN library_roots registered ON registered.id=season.root_id AND registered.library_id=season.library_id
		WHERE season.id=$1 AND season.root_id=$2 AND season.library_id=$3 AND registered.binding_revision=$4
		AND season.type='Season' AND season.is_folder AND season.relative_path=$5 AND season.path=$6 AND season.index_number=$7
		AND series.type='Series' AND series.is_folder`
	if lock {
		query += ` FOR SHARE OF season,series`
	}
	var seriesRelative, seriesPath, seriesParent string
	err := db.QueryRow(ctx, query, binding.SeasonID, binding.RootID, root.libraryID, binding.RootRevision,
		filepath.ToSlash(directory), filepath.Join(root.path, directory), binding.Numbers.Season).
		Scan(&binding.SeriesID, &seriesRelative, &seriesPath, &seriesParent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if seriesRelative == tvRootSeriesPath {
		if seriesPath != "" || seriesParent != root.libraryID || filepath.Dir(directory) != "." {
			return nil, nil
		}
	} else {
		ancestor := filepath.FromSlash(seriesRelative)
		if ancestor == "." || !validImageScanPath(ancestor) || ancestor != filepath.Clean(ancestor) ||
			!strings.HasPrefix(directory, ancestor+string(filepath.Separator)) || seriesPath != filepath.Join(root.path, ancestor) {
			return nil, nil
		}
	}
	binding.SeriesPath = seriesRelative
	return &binding, nil
}

func (source episodePairSource) checkBinding(ctx context.Context, tx pgx.Tx, expected *episodePairBinding, changed error) error {
	if expected == nil {
		return nil
	}
	current, err := source.readBinding(ctx, tx, true)
	if err != nil {
		return err
	}
	if !expected.same(current) {
		return changed
	}
	return nil
}
