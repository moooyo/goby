import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, IconButton, MenuItem, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import UploadRounded from '@mui/icons-material/UploadRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { ApiError, isAbortError } from './api';
import { artworkApi, imageTypes, multipleImages } from './artworkApi';
import type { ArtworkCollection, ArtworkImage, ArtworkTarget, ImageType } from './artworkApi';
import { ErrorNotice } from './components';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

function SavedImagePreview({ image, name }: { image: ArtworkImage; name: string }) {
  const [failed, setFailed] = useState(false);
  return failed
    ? <Box sx={{ p: 2, minHeight: 150, bgcolor: 'background.default' }}><Typography variant="body2" role="status" color="text.secondary">This image is unavailable. Reload images to check the current source and access.</Typography></Box>
    : <Box component="img" src={image.PreviewUrl} alt={`${image.ImageType} image ${image.ImageIndex + 1} for ${name}`} onError={() => setFailed(true)} sx={{ width: '100%', height: 150, objectFit: 'contain', bgcolor: 'background.default', display: 'block' }} />;
}

export function ArtworkManagerDialog({ target, onClose, onNavigationGuardChange }: { target: ArtworkTarget; onClose: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [collection, setCollection] = useState<ArtworkCollection>();
  const [type, setType] = useState<ImageType>('Primary');
  const [index, setIndex] = useState(0);
  const [file, setFile] = useState<File>();
  const [preview, setPreview] = useState('');
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [review, setReview] = useState(false);
  const [reloadRevision, setReloadRevision] = useState(0);
  const [removing, setRemoving] = useState<ArtworkImage>();
  const [resetting, setResetting] = useState(false);
  const [notice, setNotice] = useState('');
  const inFlight = useRef(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const images = collection?.Items.filter((item) => item.ImageType === type).sort((left, right) => left.ImageIndex - right.ImageIndex) ?? [];
  const appendIndex = images.reduce((next, image) => Math.max(next, image.ImageIndex + 1), 0);
  const disabled = loading || busy || review || !collection;
  const fileError = file && (!['image/jpeg', 'image/png', 'image/gif'].includes(file.type) ? 'Choose a JPEG, PNG, or GIF image.' : file.size === 0 || file.size > 20 * 1024 * 1024 ? 'Choose an image between 1 byte and 20 MiB.' : undefined);
  useUserDraftNavigation(Boolean(file), busy, onNavigationGuardChange, 'Discard the selected image and leave this page?');
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setCollection(undefined);
    void artworkApi.list(target, { signal: controller.signal }).then((result) => { if (!controller.signal.aborted) { setCollection(result); setIndex(result.Items.filter((image) => image.ImageType === type).sort((left, right) => left.ImageIndex - right.ImageIndex)[0]?.ImageIndex ?? 0); setReview(false); } })
      .catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [target.id, target.kind, reloadRevision]);
  useEffect(() => {
    if (!file || fileError) { setPreview(''); return; }
    const url = URL.createObjectURL(file); setPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [file, fileError]);
  function clearFile() { setFile(undefined); if (fileInput.current) fileInput.current.value = ''; }
  function close() { if (!inFlight.current && (!file || window.confirm('Discard the selected image?'))) onClose(); }
  function reload() { if (!inFlight.current && (!file || window.confirm('Discard the selected image and reload saved images?'))) { clearFile(); setReloadRevision((value) => value + 1); } }
  async function mutate(operation: () => Promise<ArtworkCollection>, message: string) {
    if (inFlight.current || disabled) return;
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    try { const result = await operation(); setCollection(result); clearFile(); setRemoving(undefined); setResetting(false); setIndex(result.Items.filter((image) => image.ImageType === type).sort((left, right) => left.ImageIndex - right.ImageIndex)[0]?.ImageIndex ?? 0); setNotice(message); }
    catch (cause) {
      if (!isAbortError(cause)) setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 0 || cause.status === 409 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
      setRemoving(undefined); setResetting(false);
    } finally { inFlight.current = false; setBusy(false); }
  }
  function move(position: number, delta: number) {
    if (!collection) return;
    const order = images.map((image) => image.ImageIndex);
    [order[position], order[position + delta]] = [order[position + delta], order[position]];
    void mutate(() => artworkApi.reorder(target, type, order, collection.Revision), 'Image order saved.');
  }
  return <>
    <Dialog open fullWidth maxWidth="md" onClose={close} aria-labelledby="artwork-title">
      <DialogTitle id="artwork-dialog-heading"><Typography component="span" variant="h3" id="artwork-title">{target.kind === 'users' ? 'Manage avatar' : 'Manage artwork'}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{target.name}</Typography></DialogTitle>
      <DialogContent aria-busy={loading || busy}><Stack spacing={2.5} sx={{ pt: 1 }}>
        {error != null && <ErrorNotice error={error} />}
        {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>Reload</Button>}>Images changed or the response could not be confirmed. Reload and review the saved images before making another change.</Alert>}
        {notice && <Alert severity="success">{notice}</Alert>}
        {loading && <Skeleton variant="rounded" height={180} aria-label="Loading images" />}
        {target.kind !== 'users' && <TextField select label="Image type" value={type} disabled={loading || busy || Boolean(file)} onChange={(event) => { const nextType = event.target.value as ImageType; setType(nextType); setIndex(collection?.Items.filter((image) => image.ImageType === nextType).sort((left, right) => left.ImageIndex - right.ImageIndex)[0]?.ImageIndex ?? 0); setNotice(''); }}>{imageTypes.map((value) => <MenuItem key={value} value={value}>{value}</MenuItem>)}</TextField>}
        {collection && images.length === 0 && <Paper variant="outlined" sx={{ p: 3 }}><Typography variant="body2" color="text.secondary">No {type.toLowerCase()} image is saved. Choose an image to add one.</Typography></Paper>}
        <Box component="ul" aria-label="Saved images" sx={{ m: 0, p: 0, listStyle: 'none', display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, minmax(0, 1fr))' }, gap: 2 }}>
          {images.map((item, position) => <Paper component="li" key={`${item.ImageType}-${item.ImageIndex}-${item.Tag}`} variant="outlined" sx={{ overflow: 'hidden' }}>
            <SavedImagePreview image={item} name={target.name} />
            <Box sx={{ p: 1.5 }}><Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography variant="body2" sx={{ fontWeight: 650 }}>{item.ImageType} {item.ImageIndex + 1}</Typography><Chip size="small" variant="outlined" label={item.Source} /></Stack><Typography variant="caption" color="text.secondary">{item.Width} × {item.Height} · {(Number(item.Size) / 1024).toFixed(0)} KiB</Typography>
              <Stack direction="row" sx={{ justifyContent: 'space-between', mt: 1 }}><Box>{multipleImages(type) && <><IconButton size="small" aria-label={`Move image ${position + 1} earlier`} disabled={disabled || Boolean(file) || position === 0} onClick={() => move(position, -1)}><ArrowBackRounded /></IconButton><IconButton size="small" aria-label={`Move image ${position + 1} later`} disabled={disabled || Boolean(file) || position === images.length - 1} onClick={() => move(position, 1)}><ArrowForwardRounded /></IconButton></>}</Box><IconButton size="small" aria-label={`Delete ${type} image ${item.ImageIndex + 1}`} disabled={disabled || Boolean(file)} onClick={() => setRemoving(item)}><DeleteOutlineRounded /></IconButton></Stack>
            </Box>
          </Paper>)}
        </Box>
        {collection && <><Divider /><Box component="section" aria-label="Upload image"><Stack spacing={2}>
          <Typography component="h3" variant="h4">{images.length ? 'Add or replace image' : 'Add image'}</Typography>
          {multipleImages(type) && <TextField select label="Upload destination" value={index} disabled={disabled || Boolean(file)} onChange={(event) => setIndex(Number(event.target.value))}>{images.map((item) => <MenuItem key={item.ImageIndex} value={item.ImageIndex}>Replace image {item.ImageIndex + 1}</MenuItem>)}{appendIndex < 32 && <MenuItem value={appendIndex}>{images.length ? 'Add a new image' : 'First image'}</MenuItem>}</TextField>}
          <Typography variant="body2" color="text.secondary">JPEG, PNG, or GIF, up to 20 MiB. Changes are stored by Goby; your original media files are kept.</Typography>
          <input ref={fileInput} id="artwork-file" type="file" accept="image/jpeg,image/png,image/gif" disabled={disabled} onChange={(event) => { setFile(event.target.files?.[0]); setNotice(''); }} aria-label="Choose image file" />
          {fileError && <Alert severity="error">{fileError}</Alert>}
          {file && <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{file.name} · {(file.size / 1024).toFixed(0)} KiB</Typography>}
          {preview && <Box component="img" src={preview} alt="Selected image preview" sx={{ height: 140, maxWidth: '100%', objectFit: 'contain', alignSelf: 'flex-start' }} />}
          <Stack direction="row" spacing={1}><Button variant="contained" startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <UploadRounded />} disabled={disabled || !file || Boolean(fileError)} onClick={() => { if (file && collection) void mutate(() => artworkApi.upload(target, type, index, file, collection.Revision), 'Image saved.'); }}>{busy ? 'Saving image...' : 'Upload image'}</Button>{file && <Button onClick={clearFile} disabled={busy}>Clear selection</Button>}</Stack>
        </Stack></Box></>}
        {target.kind !== 'users' && collection && <Button color="secondary" sx={{ alignSelf: 'flex-start' }} disabled={disabled || Boolean(file)} onClick={() => setResetting(true)}>Restore automatic {type.toLowerCase()} images</Button>}
      </Stack></DialogContent>
      <DialogActions sx={{ p: 3 }}><Button startIcon={<RefreshRounded />} onClick={reload} disabled={busy || loading}>Reload images</Button><Button color="secondary" onClick={close} disabled={busy}>Close</Button></DialogActions>
    </Dialog>
    {(removing || resetting) && <Dialog open maxWidth="xs" fullWidth onClose={busy ? undefined : () => { setRemoving(undefined); setResetting(false); }} aria-labelledby="confirm-image-title"><DialogTitle id="confirm-image-title">{resetting ? 'Restore automatic images?' : 'Delete this image?'}</DialogTitle><DialogContent><Typography color="text.secondary">{resetting ? 'This removes your saved overrides for this image type and uses images found in the media source.' : 'This image will no longer be displayed. Original media files are kept.'}</Typography></DialogContent><DialogActions sx={{ p: 3 }}><Button autoFocus onClick={() => { setRemoving(undefined); setResetting(false); }} disabled={busy}>Cancel</Button><Button variant="contained" color={resetting ? 'primary' : 'error'} disabled={busy} onClick={() => { if (collection) { if (removing) void mutate(() => artworkApi.remove(target, removing, collection.Revision), 'Image deleted.'); else void mutate(() => artworkApi.reset(target, type, collection.Revision), 'Automatic images restored.'); } }}>{resetting ? 'Restore automatic' : 'Delete image'}</Button></DialogActions></Dialog>}
  </>;
}
