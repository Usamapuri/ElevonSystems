/**
 * The brand for the signed-in shell: from settings once they have loaded,
 * and until then what this browser remembered, so the rail never flashes
 * "Elevon POS" on a store that has named itself. Each settings load also
 * refreshes the memory the sign-in page reads.
 */
import { useEffect, useMemo } from 'react'
import { useSettings } from '@/components/settings/useSettings'
import { brandFromSettings, browserStorage, recallBrand, rememberBrand, type Brand } from '@/lib/brand'

export function useBrand(): Brand {
  const { data: settings } = useSettings()
  const brand = useMemo(() => (settings ? brandFromSettings(settings) : recallBrand(browserStorage())), [settings])
  useEffect(() => {
    if (settings) rememberBrand(browserStorage(), brand)
  }, [settings, brand])
  return brand
}
