package transcode

// GeneratedAVPacketFrameAssociationMeasurement keeps raw capture, strict mixed
// observation and encoded-unit candidates separate. Complete means the joined
// bounded diagnostic finished. It never certifies decoded content, audible
// trim/tail/seam, source EOF, native playback or qualification. Stage contains
// only a fixed diagnostic label; no private path, argv or stderr is retained.
type GeneratedAVPacketFrameAssociationMeasurement struct {
	Qualified, Complete, NativeClockComplete bool
	Stage                                    string
	RawProjection                            GeneratedAVAssociationProjection
	Projection                               GeneratedAVPacketFrameProjection
	Transport                                GeneratedAVTransportDiagnostic
	Association                              GeneratedAVPacketFrameTransportAssociation
}
