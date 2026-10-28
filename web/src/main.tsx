import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Toaster } from 'sonner'
import './index.css'
import App from './App'
import { SessionProvider } from './lib/session'
import { TooltipProvider } from './components/ui'
import { ApiError } from './lib/api'

const queryClient: QueryClient = new QueryClient({
  // Any change to the fleet (instances, groups, hosts) can change redundancy.
  mutationCache: new MutationCache({ onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['resilience'] }) }),
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: true,
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <SessionProvider>
        <TooltipProvider>
          <App />
          <Toaster position="bottom-right" theme="system" richColors closeButton />
        </TooltipProvider>
      </SessionProvider>
    </QueryClientProvider>
  </StrictMode>,
)
