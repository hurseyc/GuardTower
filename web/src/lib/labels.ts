import type { FamilyCategory } from './api'
import type { GroupScore } from '@/components/Resilience'

export const driverLabel: Record<string, string> = {
  builtin: 'Built-in updater',
  docker: 'Docker',
  ssh: 'SSH',
  '': 'Not configured',
}

/** Method column text while AdGuard Home hasn't said whether its updater works. */
export const detectPendingLabel = 'Checked when an update is out'
export const detectPendingHelp =
  "AdGuard Home only says whether it can update itself while a newer version is available, so GuardTower can't tell yet. Nothing is wrong: when the next release comes out, this shows Built-in updater, or asks you to set up Docker or SSH."


export const categoryLabel: Record<FamilyCategory, string> = {
  adult: 'Adult',
  gambling: 'Gambling',
  bypass: 'VPN & private DNS',
  social: 'Social media',
  gaming: 'Gaming',
  malware: 'Malware',
  safesearch: 'Safe search',
  ads: 'Ads & tracking',
}

export const scoreLabel: Record<GroupScore, string> = {
  redundant: 'Redundant',
  at_risk: 'At risk',
  spof: 'Single point of failure',
  down: 'Down',
  unknown: 'Unknown',
}
