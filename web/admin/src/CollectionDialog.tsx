import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Skeleton, Stack, Switch, TablePagination, TextField, Typography } from './ui';
import { DeleteOutlineRounded } from './icons';
import { RefreshRounded } from './icons';
import { ApiError, isAbortError } from './api';
import { collectionsApi } from './collectionsApi';
import type { CollectionMember, CollectionPage, CollectionPatch, ManagedCollection } from './collectionsApi';
import { CollectionItemPicker } from './CollectionItemPicker';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

interface Draft { Name: string; IsLocked: boolean }
function draftFor(value: ManagedCollection): Draft { return { Name: value.Name, IsLocked: value.IsLocked }; }
function draftKey(draft: Draft) { return JSON.stringify([draft.Name, draft.IsLocked]); }
function uncertain(error: unknown) { return !(error instanceof ApiError) || error.status === 409 || error.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(error.code); }

export function CollectionDialog({ collectionId, onClose, onSaved, onNavigationGuardChange }: { collectionId?: string; onClose: () => void; onSaved: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t, formatCount } = useTranslation();
  const kind = 'collections';
  const label = 'collection';
  const [id, setId] = useState(collectionId);
  const [saved, setSaved] = useState<ManagedCollection>();
  const [draft, setDraft] = useState<Draft>({ Name: '', IsLocked: false });
  const [members, setMembers] = useState<CollectionPage<CollectionMember>>();
  const [page, setPage] = useState(0);
  const [membersLoading, setMembersLoading] = useState(false);
  const [membersError, setMembersError] = useState<unknown>();
  const [memberRevision, setMemberRevision] = useState(0);
  const [selected, setSelected] = useState<string[]>([]);
  const [draftMembers, setDraftMembers] = useState<CollectionMember[]>([]);
  const [loading, setLoading] = useState(Boolean(collectionId));
  const [loadError, setLoadError] = useState<unknown>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [blocked, setBlocked] = useState(false);
  const [revision, setRevision] = useState(0);
  const [pending, setPending] = useState<'close' | 'reload' | 'discard'>();
  const [deleting, setDeleting] = useState(false);
  const mutation = useRef<AbortController | undefined>(undefined);
  const dirty = saved ? draftKey(draft) !== draftKey(draftFor(saved)) : Boolean(draft.Name || draft.IsLocked);
  const disabled = loading || Boolean(busy) || blocked || loadError != null;
  const memberDisabled = disabled || dirty || !saved || saved.IsLocked;
  const needsInitialMember = !id && selected.length === 0;
  useUserDraftNavigation(dirty || selected.length > 0, Boolean(busy), onNavigationGuardChange, `Discard unsaved ${label} changes and leave this page?`);
  useEffect(() => () => mutation.current?.abort(), []);

  useEffect(() => {
    if (!id) return;
    const controller = new AbortController(); setLoading(true); setLoadError(undefined);
    void collectionsApi.get(kind, id, { signal: controller.signal }).then((value) => {
      if (!controller.signal.aborted) { setSaved(value); setDraft(draftFor(value)); setBlocked(false); setError(undefined); setSelected([]); }
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [kind, id, revision]);
  useEffect(() => {
    if (!id) return;
    const controller = new AbortController(); setMembersLoading(true); setMembersError(undefined); setMembers(undefined);
    void collectionsApi.members(kind, id, page * 25, 25, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      if (page > 0 && page * 25 >= result.TotalRecordCount) setPage(Math.max(0, Math.ceil(result.TotalRecordCount / 25) - 1));
      else setMembers(result);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setMembersError(cause); })
      .finally(() => { if (!controller.signal.aborted) setMembersLoading(false); });
    return () => controller.abort();
  }, [kind, id, page, memberRevision, revision]);

  function reload() { setPending(undefined); setDeleting(false); setNotice(''); setRevision((value) => value + 1); }
  function request(action: 'close' | 'reload') {
    if (mutation.current) return;
    if (dirty || selected.length > 0) setPending(action);
    else if (action === 'reload') reload(); else onClose();
  }
  async function mutate(label: string, action: (signal: AbortSignal) => Promise<void>) {
    if (mutation.current || disabled) return;
    const controller = new AbortController(); mutation.current = controller; setBusy(label); setError(undefined); setNotice('');
    try { await action(controller.signal); }
    catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); if (uncertain(cause)) setBlocked(true); } }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(''); }
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft.Name.trim() || !dirty || needsInitialMember) return;
    await mutate(`Saving ${label}`, async (signal) => {
      if (!id) {
        const result = await collectionsApi.create(kind, { Name: draft.Name.trim(), IsPublic: false, IsLocked: draft.IsLocked, Ids: selected }, { signal });
        if (!signal.aborted) { setSelected([]); setDraftMembers([]); setId(result.Id); setLoading(true); setNotice('Collection created with selected media.'); onSaved(); }
        return;
      }
      if (!saved) return;
      const patch: CollectionPatch = {};
      if (draft.Name !== saved.Name) patch.Name = draft.Name.trim();
      if (draft.IsLocked !== saved.IsLocked) patch.IsLocked = draft.IsLocked;
      const latest = await collectionsApi.get(kind, id, { signal });
      const conflict = (patch.Name !== undefined && latest.Name !== saved.Name) || (patch.IsLocked !== undefined && latest.IsLocked !== saved.IsLocked);
      if (conflict) throw new ApiError('These details changed after you opened them. Reload before saving again.', { status: 409, code: 'collection_changed' });
      const result = await collectionsApi.update(kind, id, patch, { signal });
      if (!signal.aborted) { setSaved(result); setDraft(draftFor(result)); setNotice(`${result.Name} saved.`); onSaved(); }
    });
  }
  function selectMembers(ids: string[], addedItem?: CollectionMember) {
    if (mutation.current || disabled) return;
    setSelected(ids);
    if (!id) setDraftMembers((current) => {
      const choices = new Map(current.map((member) => [member.Id, member]));
      if (addedItem) choices.set(addedItem.Id, addedItem);
      return ids.flatMap((memberId) => { const member = choices.get(memberId); return member ? [member] : []; });
    });
  }
  async function changeMembers(action: (signal: AbortSignal) => Promise<unknown>, message: string, clearSelection = false) {
    if (!id || memberDisabled) return;
    await mutate('Updating items', async (signal) => {
      await action(signal);
      if (signal.aborted) return;
      if (clearSelection) setSelected([]);
      setMemberRevision((value) => value + 1);
      onSaved();
      try {
        const result = await collectionsApi.get(kind, id, { signal });
        if (!signal.aborted) { setSaved(result); setDraft(draftFor(result)); setNotice(message); }
      } catch (cause) {
        if (!signal.aborted) setBlocked(true);
        throw cause;
      }
    });
  }
  const title = id ? `Manage ${label}` : `Create ${label}`;
  return <>
    <Dialog open fullWidth maxWidth="sm" className="ac-collection-dialog" onClose={() => request('close')} aria-label={t(title)} PaperProps={{ sx: { width: 'min(540px, 100vw)' } }}>
      <Box component="form" onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)}>
        <DialogTitle id="manage-collection-title"><span className="ac-collection-eyebrow">{t(title)}</span><strong className="ac-collection-title" translate="no">{draft.Name.trim() || saved?.Name || t('Untitled collection')}</strong><span className="ac-collection-subtitle">{id ? formatCount(saved?.ChildCount ?? 0, 'item', 'items') : formatCount(selected.length, 'draft item', 'draft items')}</span></DialogTitle>
        <DialogContent aria-busy={loading || Boolean(busy)}><Stack spacing={2.5} sx={{ pt: 1 }}>
          {loadError != null && <ErrorNotice error={loadError} retry={() => request('reload')} />}
          {error != null && <ErrorNotice error={error} />}
          {blocked && <Alert severity="warning">The result could not be confirmed or the saved details changed. {id ? 'Reload before making another change. Your draft is still here.' : 'Close this dialog and refresh the list before deciding whether to create another entry.'}{id && <Button size="small" color="inherit" onClick={() => request('reload')} startIcon={<RefreshRounded />} sx={{ display: 'block', mt: 1 }}>Reload latest {label}</Button>}</Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {loading && !saved && <Skeleton variant="rounded" height={260} />}
          {(!id || saved) && <>
            <TextField autoFocus fullWidth required label="Collection name" value={draft.Name} disabled={disabled} onChange={(event) => setDraft((current) => ({ ...current, Name: event.target.value }))} slotProps={{ htmlInput: { maxLength: 256 } }} />
            <FormControlLabel label="Lock membership and deletion" control={<Switch checked={draft.IsLocked} disabled={disabled} onChange={(event) => setDraft((current) => ({ ...current, IsLocked: event.target.checked }))} />} />
            {!id && <Stack component="section" spacing={2} aria-labelledby="collection-create-members-title" aria-describedby="collection-create-members-help">
              <Typography variant="body2" color="text.secondary">The name, lock setting, and selected media are saved together when you create the collection.</Typography>
              <Typography id="collection-create-members-title" className="ac-collection-section-title" component="h3">Members</Typography>
              <Typography id="collection-create-members-help" variant="body2" color="text.secondary">Select at least one media item before creating the collection.</Typography>
              {draftMembers.length > 0 && <ol className="ac-collection-members" aria-label={t('Draft collection items')} style={{ maxHeight: 260, overflow: 'auto' }}>{draftMembers.map((member, index) => <li key={member.Id}>
                <span className="ac-collection-member-number" aria-hidden="true">{index + 1}</span>
                <span className="ac-collection-member-name" translate="no">{member.Name}<span>{t(member.Type)}</span></span>
                <Button size="small" disabled={disabled} aria-label={t('Remove {name} from draft collection', { name: member.Name })} onClick={() => selectMembers(selected.filter((memberId) => memberId !== member.Id))}>Remove</Button>
              </li>)}</ol>}
              <Box><Typography className="ac-collection-section-title" component="h3" sx={{ mb: 1.5 }}>Add media</Typography><CollectionItemPicker disabled={disabled} selected={selected} onSelectedChange={selectMembers} /></Box>
            </Stack>}
            {saved && id && <Stack spacing={2.5}>
              <Typography variant="body2" color="text.secondary">Adding or removing media takes effect immediately. Save collection details separately.</Typography>
              {dirty && <Alert severity="info">Save or discard detail changes before editing items.</Alert>}
              {saved.IsLocked && <Alert severity="info">This collection is locked. Turn off Lock membership and deletion, then save.</Alert>}
              {membersError != null && <ErrorNotice error={membersError} retry={() => setMemberRevision((value) => value + 1)} />}
              <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center' }}><Typography className="ac-collection-section-title" component="h3">Members</Typography><Button size="small" startIcon={<RefreshRounded aria-hidden="true" />} disabled={Boolean(busy) || membersLoading} onClick={() => setMemberRevision((value) => value + 1)}>Refresh items</Button></Stack>
              {membersLoading && <Skeleton variant="rounded" height={120} />}
              {members?.Items.length === 0 && <Typography color="text.secondary">No members yet. Find media below to add it.</Typography>}
              {members && <ol className="ac-collection-members" start={page * 25 + 1} aria-label={t('Collection items')}>{members.Items.map((member, index) => <li key={member.Id}>
                <span className="ac-collection-member-number" aria-hidden="true">{page * 25 + index + 1}</span>
                <span className="ac-collection-member-name" translate="no">{member.Name}<span>{t(member.Type)}</span></span>
                <Button color="error" size="small" disabled={memberDisabled || membersLoading} aria-label={`Remove ${member.Name} from collection`} onClick={() => void changeMembers((signal) => collectionsApi.removeMember(kind, id, member, { signal }), `${member.Name} removed from collection.`)}>Remove</Button>
              </li>)}</ol>}
              {members && <TablePagination component="div" count={members.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} disabled={Boolean(busy) || membersLoading} onPageChange={(_event, value) => setPage(value)} sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', px: 0 }, '& .MuiTablePagination-spacer': { display: 'none' } }} />}
              <Box><Typography className="ac-collection-section-title" component="h3" sx={{ mb: 1.5 }}>Add media</Typography><CollectionItemPicker disabled={Boolean(memberDisabled)} selected={selected} onSelectedChange={setSelected} /><Button variant="contained" disabled={memberDisabled || selected.length === 0} sx={{ mt: 2 }} onClick={() => void changeMembers((signal) => collectionsApi.addMembers(kind, id, selected, { signal }), 'Selected media added.', true)}>Add selected media</Button></Box>
              <Box><Button color="error" startIcon={<DeleteOutlineRounded aria-hidden="true" />} disabled={disabled || dirty || selected.length > 0 || saved.IsLocked} onClick={() => setDeleting(true)}>Delete collection</Button><Typography variant="caption" color="text.secondary" component="p">Deleting this entry keeps the source media files.</Typography></Box>
            </Stack>}
          </>}
        </Stack></DialogContent>
        <DialogActions sx={{ px: 3, py: 2, flexWrap: 'wrap', gap: 1 }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{busy || (dirty ? 'Unsaved changes' : selected.length > 0 ? formatCount(selected.length, 'item selected for addition', 'items selected for addition') : '')}</Typography>{id && <Button startIcon={<RefreshRounded />} disabled={loading || Boolean(busy)} onClick={() => request('reload')}>Reload</Button>}{saved && dirty && <Button disabled={Boolean(busy)} onClick={() => setPending('discard')}>Discard details</Button>}<Button disabled={Boolean(busy)} onClick={() => request('close')}>Close</Button><Button type="submit" variant="contained" disabled={disabled || !dirty || !draft.Name.trim() || needsInitialMember} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{id ? 'Save details' : `Create ${label}`}</Button></DialogActions>
      </Box>
    </Dialog>
    {pending && <Dialog open onClose={() => setPending(undefined)} maxWidth="xs" fullWidth aria-labelledby="discard-collection-title"><DialogTitle id="discard-collection-title">Discard unsaved changes?</DialogTitle><DialogContent><Typography color="text.secondary">{pending === 'discard' ? 'Name and lock settings will return to the last loaded values. Selected media stays selected. Saved member changes are kept.' : 'Unsaved details and media selections will be discarded. Saved member changes are kept.'}</Typography></DialogContent><DialogActions sx={{ px: 3, pb: 2.5 }}><Button autoFocus onClick={() => setPending(undefined)}>Keep editing</Button><Button color="error" onClick={() => { if (pending === 'reload') reload(); else if (pending === 'discard' && saved) { setDraft(draftFor(saved)); setPending(undefined); } else onClose(); }}>Discard changes</Button></DialogActions></Dialog>}
    {deleting && saved && id && <Dialog open onClose={busy ? undefined : () => setDeleting(false)} fullWidth maxWidth="sm" aria-labelledby="delete-collection-title"><DialogTitle id="delete-collection-title">Delete {label}</DialogTitle><DialogContent><Stack spacing={2}><Typography>Delete <strong>{saved.Name}</strong> and its membership and sharing settings? Source media files are kept.</Typography>{error != null && <ErrorNotice error={error} />}{blocked && <Typography color="text.secondary">Reload the latest entry before trying another change.</Typography>}</Stack></DialogContent><DialogActions sx={{ px: 3, pb: 2.5 }}><Button disabled={Boolean(busy)} onClick={() => setDeleting(false)}>Cancel</Button><Button color="error" variant="contained" disabled={disabled} onClick={() => void mutate(`Deleting ${label}`, async (signal) => { await collectionsApi.remove(kind, id, { signal }); if (!signal.aborted) { onSaved(); onClose(); } })}>Delete {label}</Button></DialogActions></Dialog>}
  </>;
}
