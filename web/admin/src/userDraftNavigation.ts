import { useEffect } from 'react';

export type UserNavigationGuard = () => boolean;
export type UserNavigationGuardChange = (guard: UserNavigationGuard | undefined) => void;

export function useUserDraftNavigation(dirty: boolean, busy: boolean, onGuardChange: UserNavigationGuardChange): void {
  useEffect(() => {
    if (!dirty && !busy) {
      onGuardChange(undefined);
      return;
    }
    onGuardChange(() => !busy && window.confirm('Discard unsaved user changes and leave this page?'));
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', beforeUnload);
    return () => {
      onGuardChange(undefined);
      window.removeEventListener('beforeunload', beforeUnload);
    };
  }, [dirty, busy, onGuardChange]);
}
