import { execSync } from 'node:child_process'

export default function globalSetup() {
  // Two real AdGuard Home instances on 127.0.0.1:13001-13002.
  execSync('go run ./cmd/gtdev fleet up -n 2', { cwd: '..', stdio: 'inherit', env: { ...process.env, GTDEV_DIR: 'tmp/e2e-fleet' } })
}
