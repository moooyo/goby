import { expect, test } from '@playwright/test';

test('never caches the preview entry document and serves its referenced build', async ({ request }) => {
  for (const path of ['/', '/?preview=current', '/preview-route']) {
    const response = await request.get(path);
    expect(response.status()).toBe(200);
    expect(response.headers()['cache-control']).toBe('no-store');
    const entry = (await response.text()).match(/src="(\/assets\/index-[^"]+\.js)"/)?.[1];
    expect(entry).toBeTruthy();
    const script = await request.get(entry!);
    expect(script.status()).toBe(200);
    expect(script.headers()['content-type']).toContain('javascript');
  }
});
