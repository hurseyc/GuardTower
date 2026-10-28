import type { FamilyCategory } from './api'
import type { GroupScore } from '@/components/Resilience'

export const driverLabel: Record<string, string> = {
  builtin: 'Built-in updater',
  docker: 'Docker',
  ssh: 'SSH',
  '': 'Not configured',
}

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
