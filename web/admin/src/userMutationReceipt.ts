export function userMutationReceipt(wasAdministrator: boolean, isAdministrator: boolean, resettingPassword: boolean): string {
  if (resettingPassword) return 'Password reset for {name}. Existing sign-ins have ended.';
  if (wasAdministrator === isAdministrator) return 'Saved settings for {name}.';
  return isAdministrator ? 'Made {name} an administrator.' : 'Changed {name} to a member. Their administrator logins have ended.';
}
