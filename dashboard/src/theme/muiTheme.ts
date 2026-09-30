import { createTheme } from '@mui/material/styles'

/** Dark PDC theme — higher contrast text/surfaces for readability. */
export const muiTheme = createTheme({
  palette: {
    mode: 'dark',
    primary: { main: '#60a5fa' },
    secondary: { main: '#34d399' },
    error: { main: '#f87171' },
    warning: { main: '#fbbf24' },
    background: {
      default: '#111827',
      paper: '#1f2937',
    },
    text: {
      primary: '#f8fafc',
      secondary: '#e2e8f0',
      disabled: '#94a3b8',
    },
    divider: '#475569',
  },
  typography: {
    fontFamily: '"IBM Plex Sans", "Segoe UI", system-ui, sans-serif',
    fontSize: 14,
    h3: { fontSize: '1.05rem', fontWeight: 650, letterSpacing: '0.02em' },
    h6: { fontSize: '0.95rem', fontWeight: 650 },
    body1: { fontSize: '0.925rem', color: '#f1f5f9' },
    body2: { fontSize: '0.85rem', color: '#e2e8f0' },
    caption: { fontSize: '0.8rem', color: '#cbd5e1' },
    button: { textTransform: 'none', fontWeight: 600 },
  },
  shape: { borderRadius: 10 },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          backgroundColor: '#111827',
          color: '#f8fafc',
        },
      },
    },
    MuiCard: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          backgroundColor: '#1f2937',
          border: '1px solid #475569',
        },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
        },
      },
    },
    MuiFormLabel: {
      styleOverrides: {
        root: { color: '#e2e8f0', fontSize: '0.85rem' },
      },
    },
    MuiInputLabel: {
      styleOverrides: {
        root: { color: '#cbd5e1' },
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          backgroundColor: '#273449',
          '& .MuiOutlinedInput-notchedOutline': { borderColor: '#64748b' },
          '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: '#94a3b8' },
        },
        input: { color: '#f8fafc', fontSize: '0.875rem' },
      },
    },
    MuiSelect: {
      styleOverrides: {
        icon: { color: '#cbd5e1' },
      },
    },
    MuiMenuItem: {
      styleOverrides: {
        root: { fontSize: '0.875rem' },
      },
    },
    MuiCheckbox: {
      styleOverrides: {
        root: { color: '#94a3b8' },
      },
    },
    MuiButton: {
      styleOverrides: {
        root: { fontSize: '0.85rem' },
      },
    },
  },
})
