import assert from 'node:assert/strict';
import test from 'node:test';
import { userMutationReceipt } from './userMutationReceipt.ts';

test('account receipts describe the role actually returned by the server', () => {
  assert.equal(userMutationReceipt(false, true, false), 'Made {name} an administrator.');
  assert.equal(userMutationReceipt(true, false, false), 'Changed {name} to a member. Their administrator logins have ended.');
  for (const role of [false, true]) assert.equal(userMutationReceipt(role, role, false), 'Saved settings for {name}.');
  assert.equal(userMutationReceipt(false, false, true), 'Password reset for {name}. Existing sign-ins have ended.');
});
