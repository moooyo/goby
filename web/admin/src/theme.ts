import { createTheme } from '@mui/material/styles';

export const colors = {
  sea: '#007E87',
  deep: '#163F4B',
  ink: '#193A46',
  muted: '#5D737D',
  fog: '#D9E7EB',
  canvas: '#F3F7F8',
  paper: '#FFFFFF',
};

const displayFamily = '"Manrope Variable", "Segoe UI", sans-serif';

export const theme = createTheme({
  palette: {
    mode: 'light',
    primary: { main: colors.sea, dark: '#00656D', contrastText: colors.paper },
    secondary: { main: colors.deep },
    background: { default: colors.canvas, paper: colors.paper },
    text: { primary: colors.ink, secondary: colors.muted },
    divider: colors.fog,
    success: { main: '#257A61' },
    error: { main: '#B83843' },
    warning: { main: '#926000' },
  },
  shape: { borderRadius: 12 },
  typography: {
    fontFamily: '"Public Sans Variable", "Segoe UI", sans-serif',
    h1: { fontFamily: displayFamily, fontWeight: 720, fontSize: '2.6rem', lineHeight: 1.15, letterSpacing: '-0.055em' },
    h2: { fontFamily: displayFamily, fontWeight: 720, fontSize: '1.95rem', lineHeight: 1.25, letterSpacing: '-0.045em' },
    h3: { fontFamily: displayFamily, fontWeight: 700, fontSize: '1.25rem', lineHeight: 1.35, letterSpacing: '-0.025em' },
    h4: { fontFamily: displayFamily, fontWeight: 700, fontSize: '1.05rem', letterSpacing: '-0.015em' },
    body1: { fontSize: '0.9375rem', lineHeight: 1.65 },
    body2: { fontSize: '0.8125rem', lineHeight: 1.6 },
    button: { textTransform: 'none', fontWeight: 650, letterSpacing: 0 },
    overline: { fontSize: '0.66rem', fontWeight: 700, letterSpacing: '0.13em', lineHeight: 1.7 },
  },
  components: {
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: { root: { borderRadius: 8, minHeight: 40, paddingInline: 18 } },
    },
    MuiOutlinedInput: {
      styleOverrides: { root: { borderRadius: 8 } },
    },
    MuiPaper: {
      defaultProps: { elevation: 0 },
      styleOverrides: { outlined: { borderColor: colors.fog } },
    },
    MuiChip: {
      styleOverrides: { root: { fontWeight: 600, fontSize: '0.72rem', borderRadius: 6 } },
    },
    MuiTableCell: {
      styleOverrides: {
        root: { borderColor: colors.fog, paddingTop: 18, paddingBottom: 18 },
        head: { color: colors.muted, backgroundColor: '#F8FAFB', fontWeight: 600, fontSize: '0.72rem', paddingTop: 12, paddingBottom: 12 },
      },
    },
    MuiAlert: {
      styleOverrides: { root: { borderRadius: 8 } },
    },
    MuiDialog: {
      styleOverrides: { paper: { borderRadius: 16 } },
    },
  },
});
