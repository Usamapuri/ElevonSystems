import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import apiClient from '@/api/client'
import { toast } from '@/hooks/use-toast'
import type { AppSettings, SettingsPatch } from '@/types'

export const SETTINGS_KEY = ['settings'] as const

export function useSettings() {
  return useQuery({
    queryKey: SETTINGS_KEY,
    queryFn: async (): Promise<AppSettings> => {
      const res = await apiClient.getSettings()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load settings')
      return res.data
    },
  })
}

/** Saves a partial patch; the server returns the full map, which replaces the cache. */
export function useSaveSettings(section: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (patch: SettingsPatch) => apiClient.updateSettings(patch),
    onSuccess: (res) => {
      if (res.data) qc.setQueryData(SETTINGS_KEY, res.data)
      toast({ title: `${section} saved`, variant: 'success' })
    },
    onError: (err: unknown) => {
      toast({ title: `Could not save ${section.toLowerCase()}`, description: err instanceof Error ? err.message : 'Unknown error', variant: 'destructive' })
    },
  })
}
