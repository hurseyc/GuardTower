# Security policy

GuardTower stores admin credentials for AdGuard Home servers, so we take security reports seriously.

## Reporting a vulnerability

**Please do not open a public issue.** Use GitHub's private reporting instead: go to the repository's **Security** tab and click **Report a vulnerability**.

Include the affected version, steps to reproduce and the impact. You should get an acknowledgement within 72 hours. We aim to release a fix within 14 days for high-severity issues and will credit you in the release notes unless you prefer otherwise.

## Supported versions

Security fixes are released for the latest minor version. Please upgrade to receive them.

## Scope

In scope: the GuardTower server, its API and web UI, release artifacts and container images. Out of scope: vulnerabilities in AdGuard Home itself (report those to AdGuard), and issues that require an attacker to already hold a GuardTower admin account or access to the data directory.
