import { execSync } from 'node:child_process'

export default function globalTeardown() {
  execSync('go run ./cmd/gtdev fleet down', { cwd: '..', stdio: 'inherit', env: { ...process.env, GTDEV_DIR: 'tmp/e2e-fleet' } })
}
