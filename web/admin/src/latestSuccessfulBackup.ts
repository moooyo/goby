import type { BackupPage } from './backupsApi';

const pageSize = 100;
const maxArchives = 4096; // The server's maximum backup store object count.

export async function readLatestSuccessfulBackup(readPage: (start: number, limit: number) => Promise<BackupPage>): Promise<string | null> {
  const seen = new Set<string>();
  let start = 0;
  let total: number | undefined;
  for (;;) {
    const page = await readPage(start, pageSize);
    if (page.TotalRecordCount > maxArchives || (total !== undefined && page.TotalRecordCount !== total)
      || page.Items.length !== Math.min(pageSize, page.TotalRecordCount - start)) {
      throw new Error('The backup list changed or was incomplete.');
    }
    total = page.TotalRecordCount;
    for (const archive of page.Items) {
      if (seen.has(archive.Id)) throw new Error('The backup list changed or was incomplete.');
      seen.add(archive.Id);
    }
    // The catalog is ordered by descending creation time. UpdatedAt can change
    // when an old archive is verified again during restore planning.
    const latest = page.Items.find((archive) => archive.Kind === 'generated' && archive.State === 'ready' && archive.Verified);
    if (latest) return latest.CreatedAt;
    start += page.Items.length;
    if (start >= total) return null;
  }
}
