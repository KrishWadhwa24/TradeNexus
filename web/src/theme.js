import { createTheme } from '@mui/material/styles';

const fontMono = `"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace`;
const fontDisplay = `"Space Grotesk", -apple-system, BlinkMacSystemFont, sans-serif`;

const darkPalette = {
  primary: { main: '#6d6efc' },
  secondary: { main: '#3ecf8e' },
  background: { default: '#08090c', paper: '#0d0f13' },
  text: { primary: '#e8eaed', secondary: '#7d8590' },
  error: { main: '#f0616d' },
  warning: { main: '#e3a008' },
  success: { main: '#3ecf8e' },
  divider: '#1d2129',
};

const lightPalette = {
  primary: { main: '#5b53f0' },
  secondary: { main: '#12a56a' },
  background: { default: '#f6f7f9', paper: '#ffffff' },
  text: { primary: '#12141a', secondary: '#5b636f' },
  error: { main: '#d92d43' },
  warning: { main: '#b45309' },
  success: { main: '#12a56a' },
  divider: '#e4e7ec',
};

const getTheme = (mode) => createTheme({
  palette: {
    mode,
    ...(mode === 'dark' ? darkPalette : lightPalette),
  },
  typography: {
    fontFamily: fontMono,
    h1: { fontFamily: fontDisplay, fontWeight: 700, letterSpacing: -1.5 },
    h2: { fontFamily: fontDisplay, fontWeight: 700, letterSpacing: -1 },
    h3: { fontFamily: fontDisplay, fontWeight: 700, letterSpacing: -.5 },
    h4: { fontFamily: fontDisplay, fontWeight: 600 },
    h5: { fontFamily: fontDisplay, fontWeight: 600 },
    h6: { fontFamily: fontDisplay, fontWeight: 600 },
  },
  components: {
    MuiDrawer: {
      styleOverrides: {
        paper: {
          borderRight: `1px solid ${mode === 'dark' ? darkPalette.divider : lightPalette.divider}`,
          backgroundImage: 'none',
        }
      }
    },
    MuiAppBar: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          backgroundColor: mode === 'dark' ? 'rgba(8, 9, 12, 0.8)' : 'rgba(246, 247, 249, 0.8)',
          backdropFilter: 'blur(8px)',
        }
      }
    },
    MuiListItemButton: {
      styleOverrides: {
        root: {
          borderRadius: 8,
          "&.Mui-selected": {
            color: mode === 'dark' ? darkPalette.primary.main : lightPalette.primary.main,
            backgroundColor: mode === 'dark' ? 'rgba(109, 110, 252, 0.08)' : 'rgba(91, 83, 240, 0.08)',
            ":hover": {
              backgroundColor: mode === 'dark' ? 'rgba(109, 110, 252, 0.12)' : 'rgba(91, 83, 240, 0.12)',
            }
          }
        }
      }
    },
    MuiButton: {
      styleOverrides: {
        root: {
          borderRadius: 8,
          textTransform: 'none',
        }
      }
    },
    MuiCard: {
      styleOverrides: {
        root: {
          borderRadius: 12,
          backgroundImage: 'none',
          transition: 'transform .14s, border-color .14s',
          ":hover": {
            transform: 'translateY(-2px)',
            borderColor: 'primary.main',
          }
        }
      }
    }
  }
});

export default getTheme;
