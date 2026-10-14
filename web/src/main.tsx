import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
// Self-hosted rounded sans (Fontsource, SIL OFL — see docs/sources.md). Bundled,
// not fetched from a font CDN, so the app renders the same offline.
import '@fontsource-variable/nunito'
import { App } from './app'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 401 or 403 is a decision, not a transient failure: never retry it.
      retry: (failureCount, error) => {
        const status = (error as { status?: number })?.status
        if (status === 401 || status === 403 || status === 404 || status === 422) return false
        return failureCount < 2
      },
      refetchOnWindowFocus: false,
      staleTime: 10_000,
    },
  },
})

createRoot(document.getElementById('root') as HTMLElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
