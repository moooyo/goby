import type { Locator, Page, TestInfo } from '@playwright/test';
import { expect, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

async function expectDeviceValueFits(page: Page, device: Locator, testInfo: TestInfo, name: string, expectedText: string): Promise<void> {
  await device.locator('..').locator('..').scrollIntoViewIfNeeded();
  await evidence(page, testInfo, name);
  const layout = await device.evaluate((element) => {
    const select = element as HTMLSelectElement;
    const field = select.closest('.goby-field')!;
    const style = getComputedStyle(select);
    const text = select.selectedOptions[0]?.label ?? '';
    const context = document.createElement('canvas').getContext('2d');
    if (!context) throw new Error('Canvas text metrics are unavailable.');
    const font = style.font || `${style.fontStyle} ${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
    context.font = font;
    const letterSpacing = Number.parseFloat(style.letterSpacing) || 0;
    const paddingLeft = Number.parseFloat(style.paddingLeft);
    const paddingRight = Number.parseFloat(style.paddingRight);
    const box = (node: Element) => {
      const { left, right, top, bottom } = node.getBoundingClientRect();
      return { left, right, top, bottom };
    };
    return {
      text, font, paddingLeft, paddingRight,
      textWidth: context.measureText(text).width + Math.max(0, [...text].length - 1) * letterSpacing,
      availableWidth: select.clientWidth - paddingLeft - paddingRight,
      field: box(field), label: box(field.querySelector('.goby-field-label')!),
      input: box(select.parentElement!), help: box(field.querySelector('.goby-field-help')!),
    };
  });
  await testInfo.attach(`${name}-layout`, { body: JSON.stringify(layout, null, 2), contentType: 'application/json' });
  expect(layout.text).toBe(expectedText);
  expect(layout.availableWidth).toBeGreaterThan(0);
  expect(layout.textWidth, 'The selected device value must fit the rendered select text area.').toBeLessThanOrEqual(layout.availableWidth + 1);
  expect(layout.label.bottom, 'The device label must remain above its control.').toBeLessThanOrEqual(layout.input.top + 1);
  expect(layout.input.bottom, 'Device help and errors must remain below the control.').toBeLessThanOrEqual(layout.help.top + 1);
  expect(Math.abs(layout.input.left - layout.field.left), 'The device control must use the available field width.').toBeLessThanOrEqual(1);
  expect(Math.abs(layout.input.right - layout.field.right), 'The device control must use the available field width.').toBeLessThanOrEqual(1);
}

test.describe('English mobile device values', () => {
  test.use({ preferences: { language: 'en', theme: 'light' } });

  test('the deployment empty device value fits at narrow phone widths', async ({ page, api }, testInfo) => {
    for (const width of [375, 390]) {
      await page.setViewportSize({ width, height: 844 });
      await page.goto('/admin/settings/transcode');
      const hardware = page.getByRole('region', { name: 'Hardware acceleration', exact: true });
      const device = hardware.getByRole('combobox', { name: 'AMD device', exact: true });
      await expect(device).toBeVisible();
      await expect(device).toBeDisabled();
      await expect(device).toHaveValue('');
      await expectDeviceValueFits(page, device, testInfo, `design2-device-value-${width}-en-light-default`, 'No device selected');
      await noOverflow(page);
    }
    expect(api.writes()).toEqual([]);
  });

  test('mobile device controls retain keyboard selection and unavailable-device protection', async ({ page, api }, testInfo) => {
    const availableDevice = api.settings.Runtime!.Hardware.Devices[0];
    api.settings.Runtime!.Hardware.Devices.push({ DeviceId: 'offline-device', Label: 'Offline GPU', Available: false, Code: 'hardware_device_unavailable' });
    await page.setViewportSize({ width: 375, height: 844 });
    await page.goto('/admin/settings/transcode');
    const hardware = page.getByRole('region', { name: 'Hardware acceleration', exact: true });
    const device = hardware.getByRole('combobox', { name: 'AMD device', exact: true });
    const customize = hardware.getByRole('button', { name: 'Customize hardware acceleration', exact: true });
    await expect(device).toBeDisabled();
    await customize.focus();
    await page.keyboard.press('Enter');
    await expect(customize).toHaveAttribute('aria-pressed', 'true');
    await expect(device).toBeEnabled();
    await expect(device).toHaveValue('');
    await expect(device.getByRole('option', { name: 'Offline GPU (unavailable)', exact: true })).toBeDisabled();
    await device.focus();
    await expect(device).toBeFocused();
    await expectDeviceValueFits(page, device, testInfo, 'design2-device-value-375-en-light-custom', 'No device selected');
    await device.press('ArrowDown');
    await expect(device).toHaveValue(availableDevice.DeviceId);
    await expect(hardware.getByText('Availability checked after saving', { exact: true })).toBeVisible();
    await expectDeviceValueFits(page, device, testInfo, 'design2-device-value-375-en-light-selected', availableDevice.Label);
    await device.press('ArrowDown');
    await expect(device).toHaveValue(availableDevice.DeviceId);
    await device.press('Home');
    await expect(device).toHaveValue('');
    await hardware.getByRole('combobox', { name: 'Video decoding', exact: true }).selectOption('vaapi');
    await expect(device).toHaveAttribute('aria-invalid', 'true');
    await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
    await expectDeviceValueFits(page, device, testInfo, 'design2-device-value-375-en-light-error', 'No device selected');
    const deployment = hardware.getByRole('button', { name: 'Use deployment default for hardware acceleration', exact: true });
    await deployment.focus();
    await page.keyboard.press('Enter');
    await expect(deployment).toHaveAttribute('aria-pressed', 'true');
    await expect(device).toBeDisabled();
    await expect(device).toHaveValue('');
    await expect(hardware.getByRole('combobox', { name: 'Video decoding', exact: true })).toHaveValue('software');
    await noOverflow(page);
    expect(api.writes()).toEqual([]);
  });
});

test.describe('Chinese mobile device values', () => {
  test.use({ viewport: { width: 390, height: 844 }, preferences: { language: 'zh', theme: 'light' } });

  test('the localized deployment empty device value fits the phone field', async ({ page, api }, testInfo) => {
    await page.goto('/admin/settings/transcode');
    const hardware = page.getByRole('region', { name: '硬件加速', exact: true });
    const device = hardware.getByRole('combobox', { name: 'AMD 设备', exact: true });
    await expect(device).toBeVisible();
    await expect(device).toBeDisabled();
    await expect(device).toHaveValue('');
    await expectDeviceValueFits(page, device, testInfo, 'design2-device-value-390-zh-light-default', '未选择设备');
    await noOverflow(page);
    expect(api.writes()).toEqual([]);
  });
});
