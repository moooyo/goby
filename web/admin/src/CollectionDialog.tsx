import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, MenuItem, Paper, Skeleton, Stack, Switch, Tab, Tabs, TablePagination, TextField, Typography } from '@mui/material';
import ArrowDownwardRounded from '@mui/icons-material/ArrowDownwardRounded';
import ArrowUpwardRounded from '@mui/icons-material/ArrowUpwardRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { adminApi, ApiError, isAbortError } from './api';
import type { User } from './api';
import { collectionsApi } from './collectionsApi';
import type { CollectionKind, CollectionMember, CollectionPage, CollectionPatch, CollectionShare, ManagedCollection } from './collectionsApi';
import { CollectionItemPicker } from './CollectionItemPicker';
import { ErrorNotice } from './components';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

interface Draft { Name: string; IsPublic: boolean; IsLocked: boolean; Shares: CollectionShare[] }
function draftFor(value: ManagedCollection): Draft { return { Name: value.Name, IsPublic: value.IsPublic, IsLocked: value.IsLocked, Shares: value.Shares.map((share) => ({ ...share })) }; }
function shareKey(shares: CollectionShare[]) { return JSON.stringify([...shares].sort((a, b) => a.UserId.localeCompare(b.UserId))); }
function draftKey(draft: Draft) { return JSON.stringify([draft.Name, draft.IsPublic, draft.IsLocked, shareKey(draft.Shares)]); }
function uncertain(error: unknown) { return !(error instanceof ApiError) || error.status === 409 || error.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(error.code); }

export function CollectionDialog({ kind, collectionId, onClose, onSaved, onNavigationGuardChange }: { kind: CollectionKind; collectionId?: string; onClose: () => void; onSaved: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const label = kind === 'playlists' ? 'playlist' : 'collection';
  const [id, setId] = useState(collectionId);
  const [saved, setSaved] = useState<ManagedCollection>();
  const [draft, setDraft] = useState<Draft>({ Name: '', IsPublic: false, IsLocked: false, Shares: [] });
  const [mediaType, setMediaType] = useState('');
  const [users, setUsers] = useState<User[]>([]);
  const [usersError, setUsersError] = useState<unknown>();
  const [shareUser, setShareUser] = useState('');
  const [tab, setTab] = useState<'details' | 'items'>('details');
  const [members, setMembers] = useState<CollectionPage<CollectionMember>>();
  const [page, setPage] = useState(0);
  const [membersLoading, setMembersLoading] = useState(false);
  const [membersError, setMembersError] = useState<unknown>();
  const [memberRevision, setMemberRevision] = useState(0);
  const [selected, setSelected] = useState<string[]>([]);
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
  const dirty = saved ? draftKey(draft) !== draftKey(draftFor(saved)) : Boolean(draft.Name || draft.IsPublic || draft.IsLocked || mediaType);
  const disabled = loading || Boolean(busy) || blocked || loadError != null;
  const memberDisabled = disabled || dirty || !saved || saved.IsLocked;
  useUserDraftNavigation(dirty || selected.length > 0, Boolean(busy), onNavigationGuardChange, `Discard unsaved ${label} changes and leave this page?`);
  useEffect(() => () => mutation.current?.abort(), []);

  useEffect(() => {
    const controller = new AbortController(); setUsersError(undefined);
    void adminApi.getUsers({ signal: controller.signal }).then((result) => { if (!controller.signal.aborted) setUsers(result.Items); })
      .catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setUsersError(cause); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => {
    if (!id) return;
    const controller = new AbortController(); setLoading(true); setLoadError(undefined);
    void collectionsApi.get(kind, id, { signal: controller.signal }).then((value) => {
      if (!controller.signal.aborted) { setSaved(value); setDraft(draftFor(value)); setBlocked(false); setError(undefined); setSelected([]); setShareUser(''); }
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [kind, id, revision]);
  useEffect(() => {
    if (!id || tab !== 'items') return;
    const controller = new AbortController(); setMembersLoading(true); setMembersError(undefined); setMembers(undefined);
    void collectionsApi.members(kind, id, page * 25, 25, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      if (page > 0 && page * 25 >= result.TotalRecordCount) setPage(Math.max(0, Math.ceil(result.TotalRecordCount / 25) - 1));
      else setMembers(result);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setMembersError(cause); })
      .finally(() => { if (!controller.signal.aborted) setMembersLoading(false); });
    return () => controller.abort();
  }, [kind, id, tab, page, memberRevision, revision]);

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
    if (!draft.Name.trim() || !dirty) return;
    await mutate(`Saving ${label}`, async (signal) => {
      if (!id) {
        const result = await collectionsApi.create(kind, { Name: draft.Name.trim(), IsPublic: draft.IsPublic, IsLocked: draft.IsLocked, ...(kind === 'playlists' && mediaType ? { MediaType: mediaType } : {}) }, { signal });
        if (!signal.aborted) { setId(result.Id); setNotice(`${result.Name} created. Add media in the Items tab.`); onSaved(); }
        return;
      }
      if (!saved) return;
      const patch: CollectionPatch = {};
      if (draft.Name !== saved.Name) patch.Name = draft.Name.trim();
      if (draft.IsPublic !== saved.IsPublic) patch.IsPublic = draft.IsPublic;
      if (draft.IsLocked !== saved.IsLocked) patch.IsLocked = draft.IsLocked;
      if (shareKey(draft.Shares) !== shareKey(saved.Shares)) patch.Shares = draft.Shares;
      const latest = await collectionsApi.get(kind, id, { signal });
      const conflict = (patch.Name !== undefined && latest.Name !== saved.Name) || (patch.IsPublic !== undefined && latest.IsPublic !== saved.IsPublic)
        || (patch.IsLocked !== undefined && latest.IsLocked !== saved.IsLocked) || (patch.Shares !== undefined && shareKey(latest.Shares) !== shareKey(saved.Shares));
      if (conflict) throw new ApiError('These details changed after you opened them. Reload before saving again.', { status: 409, code: 'collection_changed' });
      const result = await collectionsApi.update(kind, id, patch, { signal });
      if (!signal.aborted) { setSaved(result); setDraft(draftFor(result)); setNotice(`${result.Name} saved.`); onSaved(); }
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
    <Dialog open fullWidth maxWidth="md" onClose={() => request('close')} aria-labelledby="manage-collection-title">
      <Box component="form" onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)}>
        <DialogTitle id="manage-collection-title">{title}</DialogTitle>
        <DialogContent aria-busy={loading || Boolean(busy)}><Stack spacing={2.5} sx={{ pt: 1 }}>
          {loadError != null && <ErrorNotice error={loadError} retry={() => request('reload')} />}
          {error != null && <ErrorNotice error={error} />}
          {blocked && <Alert severity="warning">The result could not be confirmed or the saved details changed. {id ? 'Reload before making another change. Your draft is still here.' : 'Close this dialog and refresh the list before deciding whether to create another entry.'}{id && <Button size="small" color="inherit" onClick={() => request('reload')} startIcon={<RefreshRounded />} sx={{ display: 'block', mt: 1 }}>Reload latest {label}</Button>}</Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {loading && !saved && <Skeleton variant="rounded" height={260} />}
          {(!id || saved) && <>
            {saved && <Stack direction="row" sx={{ gap: 1, alignItems: 'center', flexWrap: 'wrap' }}><Typography variant="h3" component="h2" sx={{ overflowWrap: 'anywhere' }}>{saved.Name}</Typography><Chip size="small" label={`${saved.ChildCount} items`} variant="outlined" /><Chip size="small" label={saved.IsPublic ? 'Public' : 'Private'} variant="outlined" />{saved.IsLocked && <Chip size="small" label="Locked" color="warning" variant="outlined" />}</Stack>}
            {id && <Tabs value={tab} onChange={(_event, value: 'details' | 'items') => setTab(value)} aria-label="Collection management views"><Tab value="details" label="Details and sharing" id="collection-details-tab" aria-controls="collection-details-panel" disabled={Boolean(busy)} /><Tab value="items" label="Items" id="collection-items-tab" aria-controls="collection-items-panel" disabled={Boolean(busy)} /></Tabs>}
            {tab === 'details' && <Stack spacing={2.5} role={id ? 'tabpanel' : undefined} id="collection-details-panel" aria-labelledby={id ? 'collection-details-tab' : undefined}>
              <TextField autoFocus fullWidth required label={kind === 'playlists' ? 'Playlist name' : 'Collection name'} value={draft.Name} disabled={disabled} onChange={(event) => setDraft((current) => ({ ...current, Name: event.target.value }))} slotProps={{ htmlInput: { maxLength: 256 } }} />
              {!id && kind === 'playlists' && <TextField select label="Media type" value={mediaType} disabled={disabled} onChange={(event) => setMediaType(event.target.value)}><MenuItem value="">Mixed media</MenuItem><MenuItem value="Audio">Audio</MenuItem><MenuItem value="Video">Video</MenuItem></TextField>}
              <FormControlLabel label="Public" control={<Switch checked={draft.IsPublic} disabled={disabled} onChange={(event) => setDraft((current) => ({ ...current, IsPublic: event.target.checked }))} />} />
              <Typography variant="body2" color="text.secondary">Public entries can be discovered by other users. Each user still needs access to the source media.</Typography>
              <FormControlLabel label="Lock membership and deletion" control={<Switch checked={draft.IsLocked} disabled={disabled} onChange={(event) => setDraft((current) => ({ ...current, IsLocked: event.target.checked }))} />} />
              <Typography variant="body2" color="text.secondary">Unlock and save before adding, removing, reordering, or deleting. Administrators can edit details and sharing while locked.</Typography>
              {saved && <>
                <Typography variant="caption" color="text.secondary">Owner: {users.find((user) => user.Id === saved.OwnerId)?.Name ?? saved.OwnerId}{saved.MediaType ? ` · ${saved.MediaType}` : ''}</Typography>
                <Box component="section" aria-label="Sharing"><Typography variant="h4" component="h3" sx={{ mb: 1 }}>Sharing</Typography><Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>Read access lets a user browse this entry. Edit access also allows membership changes when unlocked.</Typography>
                  {usersError != null && <ErrorNotice error={usersError} retry={() => request('reload')} />}
                  <Stack spacing={1.5}>{draft.Shares.map((share) => <Paper key={share.UserId} variant="outlined" sx={{ p: 1.5 }}><Stack direction={{ xs: 'column', sm: 'row' }} sx={{ gap: 1, alignItems: { xs: 'flex-start', sm: 'center' }, justifyContent: 'space-between' }}><Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{users.find((user) => user.Id === share.UserId)?.Name ?? `Unavailable user (${share.UserId})`}</Typography><FormControlLabel label="Can edit items" control={<Switch checked={share.CanEdit} disabled={disabled} onChange={(event) => setDraft((current) => ({ ...current, Shares: current.Shares.map((entry) => entry.UserId === share.UserId ? { ...entry, CanEdit: event.target.checked } : entry) }))} />} /><Button color="error" disabled={disabled} aria-label={`Remove sharing for ${users.find((user) => user.Id === share.UserId)?.Name ?? share.UserId}`} onClick={() => setDraft((current) => ({ ...current, Shares: current.Shares.filter((entry) => entry.UserId !== share.UserId) }))}>Remove</Button></Stack></Paper>)}</Stack>
                  {draft.Shares.length === 0 && <Typography color="text.secondary" variant="body2">No explicit shares.</Typography>}
                  <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} sx={{ mt: 2 }}><TextField select fullWidth label="Share with user" value={shareUser} disabled={disabled || usersError != null || draft.Shares.length >= 1000} onChange={(event) => setShareUser(event.target.value)}><MenuItem value="">Choose a user</MenuItem>{users.filter((user) => !user.IsDisabled && user.Id !== saved.OwnerId && !draft.Shares.some((share) => share.UserId === user.Id)).map((user) => <MenuItem key={user.Id} value={user.Id}>{user.Name}</MenuItem>)}</TextField><Button disabled={disabled || !shareUser || usersError != null || draft.Shares.length >= 1000} onClick={() => { setDraft((current) => ({ ...current, Shares: [...current.Shares, { UserId: shareUser, CanEdit: false }] })); setShareUser(''); }}>Add share</Button></Stack>
                </Box>
                <Box sx={{ pt: 2, borderTop: 1, borderColor: 'divider' }}><Button color="error" startIcon={<DeleteOutlineRounded />} disabled={disabled || dirty || selected.length > 0 || saved.IsLocked} onClick={() => setDeleting(true)}>Delete {label}</Button><Typography variant="caption" color="text.secondary" component="p" sx={{ mb: 0 }}>Deleting this entry keeps the source media files.</Typography></Box>
              </>}
            </Stack>}
            {tab === 'items' && saved && id && <Stack spacing={2.5} role="tabpanel" id="collection-items-panel" aria-labelledby="collection-items-tab">
              {dirty && <Alert severity="info">Save or discard detail changes before editing items.</Alert>}
              {saved.IsLocked && <Alert severity="info">This {label} is locked. Turn off Lock membership and deletion in Details and sharing, then save.</Alert>}
              {membersError != null && <ErrorNotice error={membersError} retry={() => setMemberRevision((value) => value + 1)} />}
              <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center' }}><Typography variant="h4" component="h3">Current items</Typography><Button startIcon={<RefreshRounded />} disabled={Boolean(busy) || membersLoading} onClick={() => setMemberRevision((value) => value + 1)}>Refresh items</Button></Stack>
              {membersLoading && <Skeleton variant="rounded" height={120} />}
              {members?.Items.length === 0 && <Typography color="text.secondary">No items in this {label}.</Typography>}
              {members && <Box component="ol" start={page * 25 + 1} aria-label="Collection items" sx={{ m: 0, pl: 3 }}>{members.Items.map((member, index) => <Box component="li" key={member.PlaylistItemId ?? member.Id} sx={{ py: 1.5, borderBottom: 1, borderColor: 'divider' }}><Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'flex-start', sm: 'center' }, justifyContent: 'space-between', gap: 1 }}><Box><Typography variant="body2" sx={{ fontWeight: 600 }}>{member.Name}</Typography><Typography variant="caption" color="text.secondary">{member.Type}</Typography></Box><Stack direction="row" sx={{ alignItems: 'center', flexShrink: 0 }}>{kind === 'playlists' && <><IconButton size="small" aria-label={`Move ${member.Name} up`} disabled={memberDisabled || membersLoading || page * 25 + index === 0} onClick={() => void changeMembers((signal) => collectionsApi.move(id, member.PlaylistItemId!, page * 25 + index - 1, { signal }), 'Playlist order updated.')}><ArrowUpwardRounded fontSize="small" /></IconButton><IconButton size="small" aria-label={`Move ${member.Name} down`} disabled={memberDisabled || membersLoading || page * 25 + index >= members.TotalRecordCount - 1} onClick={() => void changeMembers((signal) => collectionsApi.move(id, member.PlaylistItemId!, page * 25 + index + 1, { signal }), 'Playlist order updated.')}><ArrowDownwardRounded fontSize="small" /></IconButton></>}<Button color="error" size="small" disabled={memberDisabled || membersLoading} aria-label={`Remove ${member.Name} from ${label}`} onClick={() => void changeMembers((signal) => collectionsApi.removeMember(kind, id, member, { signal }), `${member.Name} removed from ${label}.`)}>Remove</Button></Stack></Stack></Box>)}</Box>}
              {members && <TablePagination component="div" count={members.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} disabled={Boolean(busy) || membersLoading} onPageChange={(_event, value) => setPage(value)} sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', px: 0 }, '& .MuiTablePagination-spacer': { display: 'none' } }} />}
              <Box sx={{ pt: 2, borderTop: 1, borderColor: 'divider' }}><Typography variant="h4" component="h3" sx={{ mb: 1.5 }}>Add media</Typography>{kind === 'playlists' && <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>Playlists can include repeated entries. Adding an item again creates another entry.</Typography>}<CollectionItemPicker disabled={Boolean(memberDisabled)} selected={selected} onSelectedChange={setSelected} /><Button variant="contained" disabled={memberDisabled || selected.length === 0} sx={{ mt: 2 }} onClick={() => void changeMembers((signal) => collectionsApi.addMembers(kind, id, selected, { signal }), 'Selected media added.', true)}>Add selected media</Button></Box>
            </Stack>}
          </>}
        </Stack></DialogContent>
        <DialogActions sx={{ px: 3, py: 2, flexWrap: 'wrap', gap: 1 }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{busy || (dirty ? 'Unsaved changes' : selected.length > 0 ? `${selected.length} items selected for addition` : '')}</Typography>{id && <Button startIcon={<RefreshRounded />} disabled={loading || Boolean(busy)} onClick={() => request('reload')}>Reload</Button>}{saved && dirty && <Button disabled={Boolean(busy)} onClick={() => setPending('discard')}>Discard details</Button>}<Button disabled={Boolean(busy)} onClick={() => request('close')}>Close</Button><Button type="submit" variant="contained" disabled={disabled || !dirty || !draft.Name.trim()} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{id ? 'Save details' : `Create ${label}`}</Button></DialogActions>
      </Box>
    </Dialog>
    {pending && <Dialog open onClose={() => setPending(undefined)} maxWidth="xs" fullWidth aria-labelledby="discard-collection-title"><DialogTitle id="discard-collection-title">Discard unsaved changes?</DialogTitle><DialogContent><Typography color="text.secondary">{pending === 'discard' ? 'Detail edits will return to the last loaded values. Selected media stays selected.' : 'Your detail edits and media selections have not been saved.'}</Typography></DialogContent><DialogActions sx={{ px: 3, pb: 2.5 }}><Button autoFocus onClick={() => setPending(undefined)}>Keep editing</Button><Button color="error" onClick={() => { if (pending === 'reload') reload(); else if (pending === 'discard' && saved) { setDraft(draftFor(saved)); setPending(undefined); } else onClose(); }}>Discard changes</Button></DialogActions></Dialog>}
    {deleting && saved && id && <Dialog open onClose={busy ? undefined : () => setDeleting(false)} fullWidth maxWidth="sm" aria-labelledby="delete-collection-title"><DialogTitle id="delete-collection-title">Delete {label}</DialogTitle><DialogContent><Stack spacing={2}><Typography>Delete <strong>{saved.Name}</strong> and its membership and sharing settings? Source media files are kept.</Typography>{error != null && <ErrorNotice error={error} />}{blocked && <Typography color="text.secondary">Reload the latest entry before trying another change.</Typography>}</Stack></DialogContent><DialogActions sx={{ px: 3, pb: 2.5 }}><Button disabled={Boolean(busy)} onClick={() => setDeleting(false)}>Cancel</Button><Button color="error" variant="contained" disabled={disabled} onClick={() => void mutate(`Deleting ${label}`, async (signal) => { await collectionsApi.remove(kind, id, { signal }); if (!signal.aborted) { onSaved(); onClose(); } })}>Delete {label}</Button></DialogActions></Dialog>}
  </>;
}
