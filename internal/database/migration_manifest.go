package database

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrMigrationIntegrity means the embedded SQL inventory differs from the
// reviewed publication manifest. It does not describe a database history hash.
var ErrMigrationIntegrity = errors.New("embedded migrations differ from the published manifest")

type publishedMigration struct {
	version int64
	name    string
	sha256  string
}

// Entries 1-28 come from the published schema-28 PostgreSQL-17 catalog. Entry 29
// appends the reviewed user-deletion SQL; its live catalog export is separate.
// Digests are never regenerated from the SQL being checked. Published entries
// are immutable; a new migration requires a reviewed manifest append.
// Existing schema_migrations rows contain no execution digest, so this manifest
// checks this release's SQL bytes without inventing historical evidence.
var publishedMigrations = [...]publishedMigration{
	{1, "0001_identity.sql", "ff241911dd14187a8195729d3b8e770ffb1daf77b06dafaf8d4d66cbab7cf0e4"},
	{2, "0002_library.sql", "ea0f3207ab54b782d372fffd44a01a261efe49ce1a6ec33e2946eda4cd52f85d"},
	{3, "0003_local_metadata.sql", "6b70754ae06d5242e2655dd607be298c53c70723a90a43571d31922fac7a5749"},
	{4, "0004_catalog_entities.sql", "f9585f2308b3866732a0869780827e7f2f26ebe5be4aafeb31fc051c508a0ea9"},
	{5, "0005_item_images.sql", "36c877e09547472b5a7a76db6877c080e4ede07286e859c574d3c2c1f77f9dbb"},
	{6, "0006_playback_state.sql", "3367f4a9c729d1b9ac64fff49f7ff94bf15be7f4bc4faf0e1f6d12e4bab2bdbd"},
	{7, "0007_client_capabilities.sql", "9419a7650a855b1d66b3b4e7a12d7767002bbd24245c1773e0d4c8737efcfe4b"},
	{8, "0008_player_state.sql", "e9eccfc1337e31a7d0d29b9aa172e107a8c27ecd231f17b05e9b6c3da18ea020"},
	{9, "0009_subtitles.sql", "4715d8b876667c3add834b763cf40c307be757847baf65e8456226a28dcee2ff"},
	{10, "0010_encoding_jobs.sql", "89350f4af91ec3faf6f64baf9fb2d0eb04f105e118d9349829dfb3063973b9a2"},
	{11, "0011_encoding_vod_plans.sql", "28528a186744bd1764cc8e0e8cf00ab3d780e77d66832a529ee7776b50b92970"},
	{12, "0012_client_playback_references.sql", "78fb1d6b2103e294c66cda61a50e7380c088066814b5c29312080f0d263c81e6"},
	{13, "0013_managed_users.sql", "48b9e8cb4be24fccf7e18b5e4e545ca9bb38dba35e46dd70bf7f129cff6fd733"},
	{14, "0014_item_metadata.sql", "645f08272ef8798774abf29cc763c37ea495dde180becfc4430e985a02cd164a"},
	{15, "0015_scan_force_probe.sql", "7e0fd4bc4ddd9a6f7d4816037f04858b1f94d731a9f6fd1035b604a815280d49"},
	{16, "0016_application_keys.sql", "6ad83dc395088262b083842624903cf78603a62fb269038b1dc5e98cdc3cb586"},
	{17, "0017_devices.sql", "5e8bff2674c99e5ac9112b3d8b8cdc950c613bf5e3e494030acc3d8fa31e5bb4"},
	{18, "0018_application_key_devices.sql", "d49056b88b9492e6ec5df772a1ca058bc1e4e7f9fdcae5c86797b07f532dc48e"},
	{19, "0019_scheduled_tasks.sql", "f9e09f2f565db518b5587556e17c6bd070e95e6b615d11faf708fc432f2740fd"},
	{20, "0020_managed_settings.sql", "143510429253806856d5e939a70cb0aea0c539b6f3d52efd8ce7f798d081776f"},
	{21, "0021_configuration_compatibility.sql", "345d6da03088e3a9f769ea26f6d65f717f2d7ca415b2cbf3aac40d2f5e279265"},
	{22, "0022_activity_entries.sql", "b1adcfb3a51d2911dc586eec678beb9940b140938382abf764615064441aa9dd"},
	{23, "0023_backup_activity.sql", "e45fba3a0b447a1df2bcef063d7eab0257096b227000dbf94bf9c8871aa01923"},
	{24, "0024_user_settings.sql", "2a9c159f111073a1c01bbbda158cfe3d98d25f417d70c7eda14b70a7798a3972"},
	{25, "0025_music_artists.sql", "3b21cac79173b4b9184d73b76066aff4199a4d6d4ad6fc64d2d0d826796b6eaf"},
	{26, "0026_theme_owners.sql", "c4b7485174fe655524fe5de6973f9d75a6ce7917f4ebd82476ac1c13be5817e8"},
	{27, "0027_movie_extras.sql", "b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c"},
	{28, "0028_storage_root_bindings.sql", "85eab266e6c7f13c34370e562a45e53ad4bdce148ff8d24c428f8ebd1505ddb0"},
	{29, "0029_user_deletion_activity.sql", "cc2bc10bdd78866854559485b05c34018b4d6ea2ec9e25c880ff234609388564"},
	{30, "0030_playlists_collections.sql", "2a87384ef8eef2425dd13d75468f8ceb2b49be043be1bb9af282c6d74743fae6"},
	{31, "0031_online_providers.sql", "751d206d5a55104b8fdc83d900fa1e18e680f0e8fe02ee308c0cad9ec13b4b64"},
	{32, "0032_management_extensions.sql", "1ffe2ef8fb7810bcbda4641e8813b77225e0130e102ef53cfbac21c05cf1fbb3"},
	{33, "0033_subtitle_styles.sql", "ab04a075a70d501e5cb11fdda8212666478f86c8a8d8fa2726d4fe5b24b36d5b"},
	{34, "0034_media_deletion_operations.sql", "6553630d31803d351859e0ad60811059b8ddc4f53914cb3de532637405c657cd"},
	{35, "0035_dynamic_playback.sql", "a69e32ca488467d69f26d5dd688525b48896c683c9b9bb42a26a0ae24910e67a"},
}

func validatePublishedMigrations(available []migration) error {
	if len(available) != len(publishedMigrations) {
		return fmt.Errorf("%w: unexpected SQL inventory", ErrMigrationIntegrity)
	}
	for index, expected := range publishedMigrations {
		item := available[index]
		if expected.version != int64(index+1) || item.version != expected.version || item.name != expected.name {
			return fmt.Errorf("%w: unexpected migration at version %d", ErrMigrationIntegrity, expected.version)
		}
		digest := sha256.Sum256([]byte(item.sql))
		if hex.EncodeToString(digest[:]) != expected.sha256 {
			return fmt.Errorf("%w: checksum mismatch at version %d", ErrMigrationIntegrity, expected.version)
		}
	}
	return nil
}
