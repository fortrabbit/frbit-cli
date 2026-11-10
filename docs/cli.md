# frbit CLI

`frbit` is the command-line interface for the [fortrabbit public API](https://api.fortrabbit.com/v1/docs). Use it to create, configure, and inspect apps and related resources from a terminal, script, or agent.

This page is the complete reference for the commands currently available in `frbit`.

## Install

Use the install script, Homebrew, npm, or a GitHub release archive. The
[installation guide](https://docs.fortrabbit.com/platform/automation/cli) covers
every supported method and platform.

Confirm the installation:

```sh
frbit version
```

For every command and option available in your installed version, run:

```sh
frbit --help
frbit apps list --help
```

## Shell completion

Homebrew installs completion automatically. For other installation methods, run
the command for your shell once:

```sh
frbit completion install bash
frbit completion install fish
frbit completion install powershell
frbit completion install zsh
```

The bash and fish installers save to their auto-discovery directories. The
PowerShell installer adds the completion to its profile, and the zsh installer
saves it in `~/.zfunc/_frbit` and adds that directory to zsh's completion path.

## Authenticate

Sign in through the browser:

```sh
frbit auth login
```

The command prints the sign-in URL and a code first, opens the URL in your default browser, and waits up to 15 minutes. Type the code on the consent page: it shows that the sign-in in the browser belongs to this terminal. Without an account, the same page signs you up. It stores the credential in your operating system's credential store and saves the API host for subsequent commands. The credential refreshes itself before it expires and is only sent to the API host it signed in to; for another host, run `frbit --host <origin> auth login`.

| Option | Effect |
|--|--|
| `--no-browser` | Print the URL without opening a browser |
| `--json` | Print `{"url": …, "confirmation_code": …, "status": "waiting"}` at once, then `{"status": "signed_in", "next": …}` |
| `--signup` | Open the sign-up page instead of the login page, for people without an account |
| `--name "<name>"` | Suggest a name for the new account, 1 to 100 characters |

If the person denies the consent request, the browser tab shows "Not connected. Your agent knows; you can close this tab." and the command fails. With `--json` the last line is `{"status": "error", "code": "access_denied", "error": …}`. The consent request is used up, so ask the person before running `frbit auth login` again. Other stable codes: `timeout` and `frbit_token_set`.

Coding agents run `frbit auth login --json` in the background, show the URL to the person, and continue with `next`. For a person without an account they add `--signup`.

Verify the active credential at any time. It shows the profile, host, credential kind, expiry, and the next setup step. With `--json` it prints one line with `"status": "signed_in"` and the same fields, `{"status": "signed_out"}` (exit 0) without a credential, or `{"status": "error", "error": …}` (exit 1):

```sh
frbit auth status
```

Remove the stored credential when you no longer need it on this machine. A browser sign-in is revoked on the server first, so it leaves the Connections list in the dashboard. When the server cannot be reached, the credential is still removed here and the sign-in stays under Connections until you disconnect it there. A stored API token is only removed locally; revoke it under API tokens. While `FRBIT_TOKEN` is set, `logout` and `auth status` warn on stderr that the token is used instead of a stored sign-in (`auth status --json` adds a `warning` field):

```sh
frbit auth logout
```

### Non-interactive authentication

Use `FRBIT_TOKEN` in CI, scripts, containers, and agents. It is used only for the current process and takes precedence over a token stored in the system credential store.

```sh
FRBIT_TOKEN=frbit-at-… frbit apps list
```

Without a browser, store a personal API token from standard input instead of signing in. `frbit auth login` without `--token-stdin` refuses to start while `FRBIT_TOKEN` is set. Avoid placing tokens directly in shell history or command-line arguments.

```sh
printf '%s' "$FRBIT_TOKEN" | frbit auth login --token-stdin
```

## Uninstall

`frbit uninstall` removes everything fortrabbit put on the machine for coding agents: the MCP server and the skill in Claude Code and Codex, the Claude Code plugin, the sign-in of the selected profile, the config and cache folders, and the binary. It lists everything and asks once; `--yes` skips the question. A Homebrew or npm install is removed with `brew uninstall frbit` or `npm uninstall --global @fortrabbit/cli`. On Windows, delete the binary yourself afterwards: a running executable cannot delete itself. Repeat `frbit auth logout --profile <name>` for other profiles before.

By hand, with the counterpart of the install method:

```sh
rm "$(command -v frbit)"                # install script or release archive
brew uninstall frbit                    # Homebrew
npm uninstall --global @fortrabbit/cli  # npm
```

`brew untap fortrabbit/tap` is only needed when no other formula from the fortrabbit tap remains. Generic binary installers remove their own copies, for example `eget --remove` or `mise uninstall`.

Two files can stay behind. `frbit/config.json` below the user config directory holds the saved API host, and `frbit/update-check.json` below the user cache directory holds the last update check. On macOS those are `~/Library/Application Support` and `~/Library/Caches`, on Linux `~/.config` and `~/.cache`, on Windows `%AppData%` and `%LocalAppData%`. Tokens are not stored in either: they sit in the operating system credential store under the service name `frbit-cli`.

## Apps

### List and inspect apps

List the apps available to the authenticated person:

```sh
frbit apps list
```

The default output is a readable table with each app's public ID, name, description, trial status, and last update time.

#### Pagination

Request a specific page of results. Pages start at `1`.

```sh
frbit apps list --page 2
```

`--limit N` returns at most N apps and fetches further pages until it has them. `--all` returns every page. Both work on every `list` command except `environments firewall-rules list` and `git branches`. `--all` cannot be combined with `--page` or `--limit`.

```sh
frbit apps list --limit 50
frbit apps list --all --json
```

With `--limit` or `--all`, `--json` prints one JSON array of the returned resources instead of the API response of a single page.

#### Filter by public ID

Use `--id` once or repeatedly to return a known set of apps:

```sh
frbit apps list --id ap-a1b2c3 --id ap-d4e5f6
```

#### Get one app

```sh
frbit apps get ap-a1b2c3
```

#### JSON output

Pass `--json` to print the API response unchanged. This is intended for scripts and tools such as `jq`.

```sh
frbit apps list --json
frbit apps list --json | jq '(."hydra:member" // .member // .)[] | .name'
```

`--json` is available on resource reads and on commands that return a created or updated resource. It prints the API response unchanged.

When the API rejects a request made with `--json`, the CLI prints the API's problem document (`title`, `detail`, `violations`) to standard output and exits non-zero. Without `--json` the error goes to standard error, one line per rejected field.

Deprecation notices from the API (`Warning`, `Deprecation` and `Sunset` headers) are printed to standard error as `warning: ...` lines, so they never mix with `--json` output.

Create commands send an `Idempotency-Key` header. When the API answers a create with 429 and asks to wait 10 seconds or less, the CLI retries it once with the same key. It never retries a create after a 5xx error, because the resource may already exist: check with a list command before running it again.

API responses can contain account data, personal information, payment metadata,
environment-variable values, and application log output. Treat terminal captures and CI logs as sensitive,
especially when using `--json` or `deployments logs`.

### Create an app

Create an app with its default environment by selecting the required component plans:

```sh
frbit apps create \
  --name acme-shop \
  --region eu-w1a \
  --software laravel \
  --software-version 11 \
  --component php=sm \
  --component storage=xs \
  --component traffic=xs \
  --component backups=xs
```

Configure Git and start the first deployment as part of the same workflow:

```sh
frbit apps create \
  --name acme-shop \
  --region eu-w1a \
  --component php=sm \
  --component storage=xs \
  --component traffic=xs \
  --component backups=xs \
  --repository acme/shop \
  --branch main \
  --build-command 'composer install --no-dev' \
  --post-deploy-command 'php artisan migrate --force' \
  --deploy
```

Use `--team` or `--payment-method` to select an accessible team or payment method. For the complete nested API payload, pass a JSON object with `--file`, or use `--file -` to read it from standard input.

The API gives the initial environment a cloud domain with a label derived from the app name, and creates none when that label is invalid, reserved or taken. Pass `--no-cloud-domain` to create the app without one, or set `initialEnvironment.cloudDomain` with `--file` to choose the label.

Pass `--root-path public` to set the folder of the repository the web server serves; it defaults to the root path of the software preset.

Pass `--deploy` to start the initial deployment as part of creation. It requires Git configuration: `apps create` needs both `--repository` and `--branch`. Without `--deploy`, creating an app does not start a deployment.

### Billed creates

`apps create` and `environments create` first run the same request as a dry run, print the monthly price and the payment method, and ask `Create and start billing? [y/N]`. The price and the question go to stderr. Trial apps without a payment method cost nothing and are created without a question. Without a terminal the command fails with `billed change needs --yes`; pass `--yes` only after the person has seen the price. The dry run applies to `--file` bodies too and sends no idempotency key.

`--dry-run` stops after that dry run: it prints the price to stdout, asks nothing, creates nothing and exits 0. It works with field flags and `--file`; with `--json` it prints the API response instead. Use it to show the price before asking the person, also on a trial account, where the real create asks nothing.

```text
$ frbit apps create --name shop --region eu-w1a --dry-run
Monthly price: 24.00 EUR
Payment method: default payment method
Dry run: nothing was created.

$ frbit apps create --name shop --region eu-w1a --dry-run   # trial account
The app would be created as a trial at no cost until 2026-11-14T10:30:00+00:00.
Dry run: nothing was created.
```

### Update an app

```sh
frbit apps update ap-a1b2c3 --name renamed-shop
frbit apps update ap-a1b2c3 --payment-method pm-a1b2c3
frbit apps update ap-a1b2c3 --git-deploy-comments=false
frbit apps update ap-a1b2c3 --software-preset laravel --confirm ap-a1b2c3
frbit apps update ap-a1b2c3 --technical-contact pn-a1b2c3 --technical-contact pn-d4e5f6
frbit apps update ap-a1b2c3 --clear-technical-contacts
frbit apps update ap-a1b2c3 --repository acme/shop --confirm ap-a1b2c3
frbit apps update ap-a1b2c3 --disconnect-repository --confirm ap-a1b2c3
```

- `--software-preset` sets the software version of every environment to the preset default and clears pinned versions; the next deployment uses that version.
- `--technical-contact` (repeatable) replaces the list with the given person public IDs; `--clear-technical-contacts` empties it.
- `--git-deploy-comments=true|false` toggles deployment comments on GitHub pull requests.
- `--repository OWNER/REPOSITORY` connects a GitHub repository (`PUT /v1/apps/{id}/git-repository`) and clears the deploy branch and deploy file settings of every environment; no environment deploys until you set a branch again with `environments update`. Combined with other flags, the app is updated first, then the repository connected.
- `--disconnect-repository` removes the connection and stops deployments until a repository is connected again.
- `--repository`, `--software-preset` and `--disconnect-repository` print what they change and ask you to type the app ID, once for all of them; without a terminal pass `--confirm <app-id>`. The check runs before any request. `--confirm` with none of these flags is an error.

## Environments

List environments, optionally select a page or specific public IDs, or retrieve one environment:

```sh
frbit environments list
frbit environments list --page 2 --id en-a1b2c3
frbit environments get en-a1b2c3
```

Create an environment with explicit component plans or clone the plans from an existing environment:

```sh
frbit environments create \
  --app ap-a1b2c3 \
  --name staging \
  --component php=sm \
  --component storage=xs \
  --component traffic=xs \
  --component backups=xs

frbit environments create \
  --app ap-a1b2c3 \
  --name preview \
  --source-environment en-a1b2c3
```

Git settings, build commands, environment variables, and the first deployment can be orchestrated during creation:

```sh
frbit environments create \
  --app ap-a1b2c3 \
  --name staging \
  --source-environment en-a1b2c3 \
  --branch main \
  --env APP_ENV=staging \
  --deploy
```

Pass `--root-path public` to set the folder of the repository the web server serves.

Pass `--deploy` to start the initial deployment as part of creation. It requires `--branch` so the environment has a Git source. Without `--deploy`, creating an environment does not start a deployment.

The API gives the environment a cloud domain with a label derived from the app's cloud-domain label, or the app name when the app has none, plus the environment name, for example `acme-shop-staging`. It creates none when that label is invalid, reserved or taken. Pass `--no-cloud-domain` to create the environment without one, or set `cloudDomain` with `--file` to choose the label.

Update the environment name or replace its deployment configuration:

```sh
frbit environments update en-a1b2c3 --name staging
frbit environments update en-a1b2c3 --branch develop --directory web
frbit environments update en-a1b2c3 --clear-build-commands
frbit environments update en-a1b2c3 --root-path public    # empty value resets it
```

Change the deployment settings. Each flag sends only its own key; repeatable list flags replace the whole list and a `--clear-<list>` flag empties it:

```sh
frbit environments update en-a1b2c3 --strategy merge --replace-pattern /vendor
frbit environments update en-a1b2c3 --exclude-pattern /storage --cached-directory vendor
frbit environments update en-a1b2c3 --clear-exclude-patterns
frbit environments update en-a1b2c3 --trigger manual --permission connectedDevelopers
frbit environments update en-a1b2c3 --node-version 24 --deploy-hook
frbit environments update en-a1b2c3 --software-version 11 --php-version 8.4   # --php-version stops automatic PHP updates
```

| Flag | Values |
| --- | --- |
| `--strategy` | `merge` or `replace` |
| `--exclude-pattern`, `--clear-exclude-patterns` | Path patterns left out of deployments |
| `--replace-pattern`, `--clear-replace-patterns` | Path patterns replaced on deployment, only with strategy `merge` |
| `--cached-directory`, `--clear-cached-directories` | Directories cached between deployments |
| `--trigger` | `push`, `manual` or `hook` |
| `--permission` | `connectedDevelopers` or `repositoryAccess` |
| `--node-version` | A version from `frbit node-versions list`; stops following newer Node.js versions |
| `--deploy-hook[=false]` | Turn the deploy hook on or off |

The strategy, the pattern lists and the cached directories are refused while a deploy file in the repository manages them. `trigger`, `permission`, `nodeVersion` and `deployHook` are always editable. `environments get` lists the settings as `deployment.<field>` rows next to `rootPath`.

Read or merge environment variables. `--set` and `--delete` are repeatable:

```sh
frbit environments variables get en-a1b2c3             # values are masked
frbit environments variables get en-a1b2c3 --reveal    # show values explicitly
frbit environments variables update en-a1b2c3 \
  --set APP_ENV=production \
  --delete OLD_FLAG
```

Values supplied directly on the command line can be retained in shell history. For sensitive values, pass the API request object using `--file` or `--file -` and standard input.

Restart an environment or trigger a deployment from its configured Git source:

```sh
frbit environments restart en-a1b2c3
frbit environments deploy en-a1b2c3
```

The `apps create`, `apps update`, `environments create`, `environments update`, and `environments variables update` commands accept `--file`. Request field flags and `--file` cannot be combined.

### PHP settings, access and components

```sh
frbit php-settings list                  # valid directives and values
frbit php-extensions list
frbit environments php-settings get en-a1b2c3
frbit environments php-settings update en-a1b2c3 --set max_execution_time=120 --set upload_max_filesize=256M --extension apcu=on

frbit environments basic-auth get en-a1b2c3
printf '%s\n' "$PASSWORD" | frbit environments basic-auth update en-a1b2c3 --enabled --username preview --password-stdin

frbit environments firewall-rules list en-a1b2c3
frbit environments firewall-rules add en-a1b2c3 --description "Metrics exporter" --port 8080-8090
frbit environments firewall-rules delete fr-a1b2c3 --confirm fr-a1b2c3

frbit environments components get en-a1b2c3
frbit environments components update en-a1b2c3 --component php=md --component database=off --yes
```

- Directives and extensions you do not name stay unchanged. Numeric `--set` values are sent as numbers.
- The basic auth password is read as one line from stdin and is never a flag or part of any output. Pipe it in; a password typed on a terminal is echoed. Omit `--password-stdin` to keep the current password.
- `--port N` opens one port, `--port N-M` a range of at most 1000 ports, within 1-65535.
- `firewall-rules delete` needs the exact rule ID: typed on a terminal, or `--confirm <id>` without one.
- `components update` looks up each size in the catalog first (an unknown size is an error before any request; a changed component whose old or new price is unknown fails with `price for <component> <size> unknown; check frbit plans list or change it in the dashboard`, before the question and also with `--yes`, so nothing is booked at an unseen price), prints `slug: old (price) -> new (price)` per changed component and the signed `Monthly change` to stderr (priced in the region of the environment's app; `--region` overrides, and the command fails before any change if the region can't be resolved), skips components already at the requested size (nothing changes: `No component plan changes.`), and asks `Change component plans? [y/N]`, also for a downsize. Without a terminal it fails with `billed change needs --yes`. The change is applied asynchronously (202).

### Metrics

```sh
frbit environments metrics en-a1b2c3
frbit environments metrics --metric php-requests   # the environment of the checkout
frbit environments metrics en-a1b2c3 --metric php-requests --range 7d
frbit environments metrics en-a1b2c3 --metric web-storage --range 30d --step hour
```

Without `--metric`, `metrics` prints one row per booked component (storage, database, traffic) with current usage, limit and percentage, the last 24 hours (requests, 500/503/504 responses, response times, PHP memory, out-of-memory kills) and the available metric slugs. With `--metric <slug>` it prints the history: totals and one point per step. `--range 1h|24h|7d|30d` (default `24h`) sets `from` and `to` (ISO 8601, UTC) counted back from now; `--step hour|day` defaults to `hour` for 1h and 24h and `day` for 7d and 30d (the API allows hourly steps up to 83 days). `--json` prints the raw API body.

### Logs

```sh
frbit environments logs                       # the environment of the checkout
frbit environments logs production            # a label from .fortrabbit/environments.yml
frbit environments logs en-a1b2c3 --source access --status 5xx --since 1h
frbit environments logs en-a1b2c3 --source php --follow
```

Prints the newest `--limit` entries (default 100, max 1000) of one `--source` (`php` default, `apache`, `access`, `jobs`), oldest first, paging back through the `Link` header. `--since` takes a duration (`15m`, `24h`, `7d`) or an ISO 8601 time with a time zone. `--status` (`500`, `5xx`, `500,503`) and `--method` filter the access log only. `--follow` polls every 2 seconds for new entries until Ctrl-C, which exits 0. Control characters other than tabs are stripped. `--follow` retries after 429, 503 and network errors (waiting `Retry-After` when given). `--json` prints the raw API body when one page satisfies the request, the merged array (newest first) when several pages were needed, and one object per line with `--follow`.

## Deployments

Inspect deployments and retrieve the log entries for one deployment. The list shows the newest 30 first; `--environment` narrows it to one environment:

```sh
frbit deployments list
frbit deployments list --environment en-a1b2c3 --page 2
frbit deployments list -e production
frbit deployments get dp-a1b2c3
frbit deployments logs dp-a1b2c3
frbit deployments logs dp-a1b2c3 --follow
```

`logs --follow` polls every 2 seconds, prints each line once and stops when the deployment is `succeeded`, `failed`, `canceled` or `unknown` and its logs are complete (`ready: true`). `--follow` retries after 429, 503 and network errors and prints `waiting for logs…` to stderr once while none have arrived. Without `--follow`, logs that are still being collected say so.

## Domains

```sh
frbit domains list
frbit domains list --page 2 --id do-a1b2c3
frbit domains get do-a1b2c3
frbit domains create en-a1b2c3 www.example.com --main
frbit domains create production acme-shop --cloud
frbit domains dns do-a1b2c3 --refresh
```

`domains create <environment> <name>` adds a domain you own (`name` is a hostname) and prints the DNS record to set at your DNS provider, the record found in DNS now, and the verification state. A bare apex such as `example.com` also creates `www.example.com`. `--main` makes it the environment's main domain.

With `--cloud`, `name` is a label such as `acme-shop`, not a full hostname; the API appends the cloud domain root. Cloud domains need no DNS record. An environment can have one cloud domain.

`domains dns <id>` shows the expected and actual record. `--refresh` makes the API check DNS again first. Cloud and default frbit.app domains have no DNS record and answer 404. Domains are not billed. `--json` prints the raw API body (for `create`, the created domain only).

## Jobs

```sh
frbit jobs list
frbit jobs list en-a1b2c3
frbit jobs get jo-a1b2c3
frbit jobs create en-a1b2c3 --name queue --type worker --command "php artisan queue:work"
frbit jobs create en-a1b2c3 --name nightly --type cron --command "php artisan schedule:run" --interval day --max-runtime 3600
frbit jobs create en-a1b2c3 --name sync --type cron --command "php sync.php" --interval custom --expression "0 */2 * * *" --max-runtime 300
frbit jobs update jo-a1b2c3 --inactive
frbit jobs delete jo-a1b2c3 --confirm jo-a1b2c3
```

`jobs list [environment]` prints the jobs and a footer `Jobs: <count> of <limit>` from the `X-Jobs-Count` and `X-Jobs-Limit` headers of the first page; `--limit` and `--all` follow further pages. The limit is 0 when no jobs plan is booked (create then answers 403). A worker runs all the time (a Laravel or Craft queue worker); a cron job runs on a schedule.

`create` needs `--name`, `--command` (must start with `php ` or `bash `) and `--type worker|cron`. A cron job also needs `--interval minute|hour|day|custom` and `--max-runtime 300|1800|3600|10800|21600`; `--interval custom` needs `--expression`. The CLI sends the flags as typed; the API checks the values and names each rejected field. Optional: `--inactive`, `--exit-timeout 1|5|10|30`, `--exit-signal TERM|INT`.

`update <job>` takes the same flags plus `--active`, and sends only the flags you pass. `delete <job>` stops and removes the job; it needs the job ID typed on a terminal or `--confirm <id>` without one. `--json` prints the raw API body.

## MySQL

```sh
frbit mysql get en-a1b2c3
frbit mysql rotate-password en-a1b2c3 --confirm en-a1b2c3
```

`mysql get [environment]` prints host, port, user and database; it never prints a password (`--json` prints the raw API body). `rotate-password` starts a password rotation; the app breaks until it reads the new password (redeploy or restart). It needs the environment ID typed on a terminal or `--confirm <id>` without one. The new password is never printed: the app reads it from its environment variables.

## Database pull

```sh
frbit db pull
frbit db pull en-a1b2c3 --output ./dumps/
frbit db pull en-a1b2c3 --stdout | gzip > db.sql.gz
```

`db pull [environment]` dumps the database of an environment to a local gzipped file and never touches a local database: import the file yourself, or let ddev do it. Without an argument it uses the environment of the checkout (see [Link a local project](#link-a-local-project)).

It runs the local `ssh` with your own SSH key; `mysqldump --single-transaction --set-gtid-purged=OFF --no-tablespaces` runs on the environment, so no database credentials reach your machine. Without a terminal, ssh runs with `BatchMode=yes` and fails instead of asking for a password.

The default output is `<environment>.sql.gz` in the current folder; `--output` is a file or a folder. The dump goes to a temporary file in the target folder that is renamed on success and removed on failure or Ctrl-C, with mode 0600. An existing file is not overwritten unless you pass `--force`. An empty dump fails. `--stdout` writes plain SQL to stdout instead.

A rejected SSH key or a failed connection (ssh exit 255) points to <https://dash.fortrabbit.com/new/ssh-key>. A missing `ssh` client fails before any request.

The host key of fortrabbit's production SSH endpoints (`ssh.<region>.frbit.app`) ships with frbit: `ssh-ed25519`, fingerprint `SHA256:UB3p/YgtQSuK3WESB/0ra5dTDWkggrCOz0WkWe4Tvvg`. frbit writes it to `known_hosts` in its config folder (mode 0600, rewritten when it differs) and checks these hosts against that file alone, with `StrictHostKeyChecking=yes`. The first connection from an agent or CI needs no prompt and no entry in `~/.ssh/known_hosts`. Entries for these hosts in your own `known_hosts` files are not used, because ssh accepts a key found in any listed file. If the server shows another key, the command stops: do not accept the key by hand; update frbit, and contact fortrabbit support if it persists.

Other hosts keep your ssh setup. When their host key is not in your `known_hosts` yet (`Host key verification failed`, common on the first connection without a terminal), the error says to run `ssh <target> exit` once in a terminal. This applies to `db push` and `files` too.

## Database push

```sh
frbit db push dump.sql.gz en-a1b2c3
frbit db push dump.sql --confirm en-a1b2c3
gunzip -c dump.sql.gz | frbit db push - en-a1b2c3 --confirm en-a1b2c3
```

`db push <file|-> [environment]` replaces the database of an environment with a local dump: every table in the dump is dropped and recreated; tables that exist only on the environment stay. The file is plain SQL (gzipped while uploading) or gzipped SQL; `-` reads it from stdin and is refused when stdin is a terminal. An empty file, a `.gz` file that is not gzip data, or a damaged gzip file fails before any request. Without an environment it uses the environment of the checkout (see [Link a local project](#link-a-local-project)).

It needs the environment ID typed on a terminal, or `--confirm <id>` without one or when the dump comes from stdin. The check runs before any SSH call; with a label or the environment of the checkout, the environment may already have been read from the API to resolve it.

Each step is its own SSH call with your own SSH key:

1. Backup: `mysqldump` of the current database to `/data/home/frbit-backup-<UTC time>.sql.gz` on the environment, written as `.part` and renamed when complete. When it fails or is cancelled, the partial file is removed and the database is not touched. When the connection drops (ssh exit 255), the removal is tried over a new connection; if that cannot connect either, `frbit-backup-<UTC time>.sql.gz.part` may stay in `/data/home/`. It never carries the backup name; remove it once you can connect.
2. Upload to `/data/home/frbit-import.sql.gz` (leftover uploads of the agent skill's `db-up.sh` are removed first).
3. `gzip -t` of the upload. A damaged upload imports nothing.
4. Import: `gunzip -c | mysql "$FORTRABBIT_DB_NAME"`.
5. Removal of the upload.

A failure or Ctrl-C after the backup says whether the database changed and prints the backup path and the command to restore it, `ssh <target> 'set -o pipefail; gunzip -c /data/home/frbit-backup-<UTC time>.sql.gz | mysql "$FORTRABBIT_DB_NAME"'`. Ctrl-C during the import leaves the database partly imported. The backup stays on the environment; remove it once you no longer need it. A rejected SSH key (ssh exit 255) points to <https://dash.fortrabbit.com/new/ssh-key>.

## Files

```sh
frbit files pull web/uploads -e en-a1b2c3 --dry-run
frbit files pull storage -e production --delete --confirm en-a1b2c3
frbit files push web/uploads --environment en-a1b2c3 --confirm en-a1b2c3
```

`files pull [path]` copies files from `/data/www/` on an environment into the current folder; `files push [path]` copies them the other way. Both run the local `rsync` over `ssh` with your own SSH key; the API token only resolves the environment and its SSH host. The environment comes from `--environment`/`-e` (ID or label) or from the checkout (see [Link a local project](#link-a-local-project)).

The path is relative to `/data/www/` and to the current folder; without one, all of `/data/www/` syncs with the current folder. A folder path syncs onto the same-named folder on the other side: `web/uploads` syncs `/data/www/web/uploads` with `./web/uploads`, not into `./web/uploads/uploads`. A file path syncs that file. Absolute paths, `..`, `~` and characters other than letters, digits and `. _ - + @ /` are refused before any request. On push, the parent folder of the path must exist on the environment.

Changed files are overwritten on the receiving side. `--delete` also removes files there that the sending side does not have. `--dry-run` lists what would change and copies nothing. rsync always runs with `--exclude=/.git/ --exclude=/.fortrabbit/ --exclude=/.env --exclude=/.env.* --exclude=.frbit-* --exclude=en-*.sql.gz --exclude=ba-*-database.* --exclude=ba-*-files.*`. The last four match at any depth, so no sync copies or deletes the dumps and backups that `frbit db pull` and `frbit backups download` save under their default names, or their temp files. The first four are anchored at the transfer root, so a whole-folder sync (no path) never copies or deletes the repository, the deploy file, the link file or the local `.env` files; inside a synced subfolder they do not apply.

A sync without a path, and any sync with `--delete`, refuses to run from `/`, your home folder, or a subfolder of a git checkout; the error names the project root to run it from. The confirmation names the absolute local folder.

`files push`, `files pull` without a path, and `files pull --delete` need the environment ID typed on a terminal, or `--confirm <id>` without one; a dry run needs neither. The check runs before rsync starts; with a label or the environment of the checkout, the environment may already have been read from the API to resolve it. When the environment deploys from a Git branch, the warning says that the next deployment overwrites code pushed this way.

rsync 3.1 and later shows overall progress (`--info=progress2`); the older rsync and openrsync that macOS ships show per-file progress. Missing `rsync` or `ssh` fails with how to install it. A rejected SSH key (exit 255) points to <https://dash.fortrabbit.com/new/ssh-key>; any other rsync failure names rsync's exit code. Ctrl-C stops rsync and says the sync stopped partway.

## People

```sh
frbit people list
frbit people list --id pn-a1b2c3
frbit people get pn-a1b2c3
```

## Teams

```sh
frbit teams list
frbit teams get tm-a1b2c3
```

## Payment methods

```sh
frbit payment-methods list
frbit payment-methods get pm-a1b2c3
```

## Catalog

Look up the values `apps create` needs instead of guessing them. These lists are not paginated; `--limit` and `--all` are accepted for consistency.

```sh
frbit regions list
frbit plans list --component php --region eu-w1a --currency EUR
frbit presets list
frbit software list
frbit php-versions list
frbit php-extensions list
frbit node-versions list
```

`plans list` accepts `--region` and `--currency` (`EUR` or `USD`) and filters client-side with `--component <slug>`. All catalog lists accept `--json`.

## Git repositories

```sh
frbit git repositories
frbit git branches acme/site
```

## Backups

```sh
frbit backups list en-a1b2c3
frbit backups create en-a1b2c3
frbit backups get ba-a1b2c3
frbit backups restore ba-a1b2c3 --confirm en-a1b2c3
frbit backups download ba-a1b2c3 --type database --output ./backups/
```

`create` requests a manual backup; it appears in `list` once completed. `list` and `get` take `--json`; `list` also takes `--limit` and `--all`.

`restore` overwrites the live database and files of the environment with the backup and cannot be undone. It reads the backup first and shows its environment (name and ID) and creation date; a backup without an environment fails there. It asks you to type the environment ID, or takes `--confirm <environment-id>` without a terminal; a missing or wrong value fails before the restore request, and a backup ID passed to `--confirm` is refused with the environment ID to use. `--json` prints the API response of the restore request.

`download` asks the API for a signed link (`--type files|database`, default `files`) and streams it to disk without sending the API token to the storage host. When the API is reached over https, the link and every redirect must be https too: an http link fails with `the backup link is not https; refusing to download`, a redirect to http with `the download redirected to plain http; refusing`, and neither error prints the signed link. Against a plain http API (local development, `--host http://localhost:8085`) http links are allowed. Downloads follow at most 10 redirects. `--output` is a file, a folder, or `-` for stdout; the default is `<backup>-<type>` plus the extension from the storage response, in the current folder. The data goes to a temporary file in the target folder that is renamed on success and removed on failure. The file is written with mode 0600 because a backup holds database dumps. An existing target file is not overwritten unless you pass `--force`. `--output -` may leave partial output on stdout when the transfer fails. The link expires; run the command again for a fresh one.

## Choosing the environment

Every command that works on one environment takes it the same way: an ID (`en-a1b2c3`) or a label from `.fortrabbit/environments.yml` (see [Link a local project](#link-a-local-project)).

- Commands that only read (`environments get`, `logs`, `metrics`, `variables get`, the settings `get`/`list` subcommands, `jobs list`, `backups list`, `mysql get`, `db pull`) may omit it. They then use the environment of the checkout, in this order: `FORTRABBIT_ENVIRONMENT`, the file entry for the current branch, the environment deploying the current branch of the connected repository, the only entry in the file. When none applies, they fail with `no environment for this checkout; pass the environment ID or run frbit link <env-id>`, on a terminal too.
- Commands that change something (`environments update`, `restart`, `deploy`, `variables update`, the settings `update`/`add` subcommands, `components update`, `jobs create`, `backups create`, `mysql rotate-password`, `domains create`) need it named. Without one, or with an empty one, they fail with `name the environment: frbit <command> <env-id|label>`.
- `deployments list -e` takes an ID or a label; without `-e` it lists every deployment and never reads the checkout.
- `db push` and `files pull|push` keep using the checkout, guarded by the `--confirm <en-id>` check. `files` takes the environment with `--environment`/`-e`, because its argument is the path.
- `--confirm` and the typed confirmation always compare with the resolved `en-` ID, never the label.

When the environment comes from a label or the checkout, one line on stderr says which; `--json` output on stdout stays clean:

```text
→ en-a1b2c3 (production, from .fortrabbit/environments.yml)
→ en-a1b2c3 (from FORTRABBIT_ENVIRONMENT)
→ en-a1b2c3 (from the repository acme/site, branch main)
```

## Link a local project

```sh
frbit link
frbit link --json
frbit link en-a1b2c3
frbit link en-d4e5f6 --label staging
```

Commands find the environment of the current checkout as described in [Choosing the environment](#choosing-the-environment).

The server lookup matches the GitHub `origin` remote to the repository connected to an app, and the current branch to the environment that deploys it. On such a connected repository `frbit link` confirms the match and writes nothing.

Bare `frbit link` runs the same steps, except the argument, and prints the environment and the step that found it. `--json` prints it as one JSON object:

```json
{"environment":"en-a1b2c3","source":"repository","repository":"acme/site","branch":"main","app":"ap-a1b2c3","name":"production","sshHost":"en-a1b2c3@ssh.eu-w1a.frbit.app","region":"eu-w1a"}
```

`source` is `env` (`FORTRABBIT_ENVIRONMENT`), `file` (with `label`) or `repository` (with `repository`, `branch` and `app`). `name`, `sshHost` and `region` come from the API. Without an environment, `--json` fails.

`.fortrabbit/environments.yml` lives at the top of the git checkout, so every command finds it from any subfolder; outside git it is read from and written to the current folder.

Without a connection, `frbit link <environment>` writes `.fortrabbit/environments.yml`, a flat map of label to environment ID. The label defaults to the current branch; `--label` sets another one. Other entries stay; commit the file. `.fortrabbit/deploy.yml` is never touched.

```yaml
main: en-a1b2c3
staging: en-d4e5f6
```

An ID outside your account fails, for example in a fork of a template. When it comes from an entry in `.fortrabbit/environments.yml`, every command (`link`, `link --json`, `db`, `files`) names the entry and the fix: relink with `frbit link <env-id> --label <label>`, or edit the file. An ID passed directly points to `frbit environments list`. A `.fortrabbit` file (with `app-env-id=`, written by an older fortrabbit skill) blocks the folder: every command that reads the checkout fails with `.fortrabbit is a link file from an older fortrabbit skill; delete it and run frbit link <env-id>`.

## Deleting resources

Apps, environments, domains, jobs, teams, and payment methods can be deleted. The CLI retrieves the resource first, displays the consequences, and requires the exact public ID before sending the irreversible request:

```sh
frbit apps delete ap-a1b2c3
frbit environments delete en-a1b2c3
frbit domains delete do-a1b2c3
frbit jobs delete jo-a1b2c3
frbit teams delete tm-a1b2c3
frbit payment-methods delete pm-a1b2c3
```

For intentional non-interactive use, repeat the public ID with `--confirm`. A missing or mismatched value prevents the deletion:

```sh
frbit apps delete ap-a1b2c3 --confirm ap-a1b2c3
```

Deleting an app or environment permanently erases its files and database contents. Deleting a payment method or team can also delete connected apps and environments.

### Exit codes for confirmations

Every confirmation works the same way, for deletes, billed creates and changes, `apps update`, `db push`, `files`, `mysql rotate-password`, `backups restore`, and the agent removals (`uninstall`, `setup agent --remove`, `mcp remove`, `skills remove`):

| Outcome | Output | Exit code |
|---|---|---|
| Confirmed, or the matching `--confirm`/`--yes` | the command's result | 0 |
| Declined: `n`, an empty answer, or a wrong ID typed | `Aborted.` on stderr, no error line | 1 |
| No terminal and no `--confirm`/`--yes` | `error: …` naming the flag to pass | 1 |
| Ctrl-C at the prompt | `interrupted` | 130 |

A script can therefore test the exit code: `frbit backups restore ba-… && ./notify.sh` runs the second command only after a restore was started.

## Agent setup

Set up the complete fortrabbit agent integration with one command:

```sh
frbit setup agent
```

The command detects Claude Code and Codex, registers the remote fortrabbit MCP
server with each detected agent, and installs the latest fortrabbit skills. It
uses each agent's native CLI to update only the MCP entry named `fortrabbit` and
prints every skills and MCP configuration path it touched. Re-running the
command is safe: current MCP entries are left unchanged and the skills are
refreshed from their independently versioned release.

The install script runs the same step right after installing when given
`--setup-agent`, or `--agent <name>` for one agent:

```sh
curl -fsSL https://github.com/fortrabbit/frbit-cli/releases/latest/download/install.sh | sh -s -- --setup-agent
```

Select one or more agents explicitly to bypass detection:

```sh
frbit setup agent --agent codex
frbit setup agent --agent claude-code --agent codex
```

The setup summary reports whether a public API credential is configured for the
selected `frbit` profile. That credential is separate from the MCP server's
OAuth authorization. Codex authorization can be started with
`codex mcp login fortrabbit`; in Claude Code, use `/mcp`.

`setup agent` is user-wide and supports Claude Code and Codex. To install only
the skills, including project-scoped skills or GitHub Copilot skills, use the
`frbit skills` commands below.

When Claude Code has the fortrabbit plugin enabled, `setup agent` and
`mcp install` skip Claude Code: the plugin brings its own skill and MCP server.

Undo the setup with `--remove`. It lists the MCP entries, skills and the CLI
sign-in it will remove and asks before it does; `--yes` skips the question. The
binary and the plugin stay; `frbit uninstall` removes them as well:

```sh
frbit setup agent --remove
frbit setup agent --remove --agent codex --yes
```

## MCP server

Manage the MCP registration independently when you do not want to change the
skills:

```sh
frbit mcp install
frbit mcp list
frbit mcp remove
```

All three commands accept repeatable `--agent claude-code|codex`. Without that
option, the CLI detects installed agents. `install` creates the named entry or
updates a stale fortrabbit URL while leaving all other MCP servers alone.
`remove` lists the affected agent configuration files and asks for confirmation;
pass `--yes` only for intentional unattended removal.

The remote endpoint is `https://mcp.fortrabbit.com/mcp`. Claude Code stores its
user-scoped entry in `~/.claude.json`; Codex stores it in
`~/.codex/config.toml`.

## Agent skills

Install the latest release of the
[fortrabbit agent skills](https://github.com/fortrabbit/agent-skills):

```sh
frbit skills install
```

A user-wide install detects existing Claude Code (`~/.claude`) and Codex
(`~/.codex`) configuration directories. Codex skills are written to its
user-skill location at `~/.agents/skills`. Select an agent explicitly to bypass
detection. Repeat `--agent` to install for more than one agent. GitHub Copilot
is never detected: select it with `--agent copilot` to write its user skills to
`~/.copilot/skills`.

```sh
frbit skills install --agent claude-code
frbit skills install --agent claude-code --agent codex
frbit skills install --agent copilot
```

Install into the current project with `--project`. With no explicit agent, a
project install writes the skills for Claude Code (`.claude/skills`), Codex
(`.agents/skills`) and GitHub Copilot (`.github/skills`).

Install and update remove fortrabbit skills that the release no longer ships,
and the `.github/instructions/fortrabbit.instructions.md` file written by
earlier releases for Copilot.

```sh
frbit skills install --project
frbit skills install --project --agent copilot
```

Installed skills retain their version independently of the `frbit` CLI. List
the CLI version together with installed skill versions and exact target paths,
update the skills, or remove them:

```sh
frbit skills list
frbit skills update
frbit skills remove
```

Pass `--project` or `--agent` to those commands to select the same scope and
targets used during installation. `remove` prints every path and asks for
confirmation. Use `--yes` only when an unattended removal is intentional.

## Profiles

Profiles let you keep separate stored credentials for different accounts or environments. The default profile is named `default`.

Pass `--profile` before the command group to select a profile:

```sh
frbit --profile work auth login
frbit --profile work apps list
frbit --profile work auth logout
```

`FRBIT_TOKEN` overrides the stored token for every profile. This makes it suitable for automation, where a token is normally supplied by the job's secret store instead of being saved on disk.

## Command reference

| Command | Description |
| --- | --- |
| `frbit auth login` | Sign in through the browser. Accepts `--no-browser`, `--json`, `--signup`, `--name`, and `--token-stdin` for a personal API token. |
| `frbit auth status` | Validate the credential selected by `--profile` or `FRBIT_TOKEN` and show its kind, expiry, and the next setup step. Accepts `--json`. |
| `frbit auth logout` | Revoke a browser sign-in and remove the stored credential for the selected profile. |
| `frbit apps list` | List apps. Accepts `--page`, `--limit`, `--all`, repeatable `--id`, and `--json`. |
| `frbit apps get <id>` | Get an app. |
| `frbit apps create` | Create an app and its initial environment. Accepts field flags or `--file`. Shows the monthly price and asks first; `--yes` skips the question. `--dry-run` only prints the price. |
| `frbit apps update <id>` | Update an app: name, payment method, software preset, technical contacts, deployment comments, repository connection. Accepts field flags or `--file`. |
| `frbit apps delete <id>` | Permanently delete an app. |
| `frbit environments list` / `get [environment]` | List or get environments. Environment arguments everywhere: see [Choosing the environment](#choosing-the-environment). `list` accepts `--page`, `--limit`, `--all`, and repeatable `--id`. |
| `frbit environments create` | Create and optionally configure/deploy an environment. Accepts field flags or `--file`. Shows the monthly price and asks first; `--yes` skips the question. `--dry-run` only prints the price. |
| `frbit environments update <environment>` | Update an environment and its deployment configuration, `--software-version`, `--php-version`. |
| `frbit environments php-settings get [environment]` / `update <environment>` | Read or change php.ini settings (`--set KEY=VALUE`) and extensions (`--extension NAME=on\|off`). |
| `frbit environments basic-auth get [environment]` / `update <environment>` | Read or change basic auth; the password only via `--password-stdin`. |
| `frbit environments firewall-rules list [environment]` / `add <environment>` / `delete <id>` | Manage opened ports; `add` takes `--description` and `--port N[-M]`, `delete` needs `--confirm <id>` without a terminal. |
| `frbit environments components get [environment]` / `update <environment>` | Read or change component sizes (`--component SLUG=SIZE`); update shows prices and needs `--yes` without a terminal. |
| `frbit environments delete <id>` | Permanently delete an environment. |
| `frbit environments variables get [environment]` | Get custom and platform-injected environment variables. |
| `frbit environments variables update <environment>` | Set or delete custom environment variables. |
| `frbit environments restart <environment>` | Request an environment restart. |
| `frbit environments deploy <environment>` | Create a deployment from the configured Git source. |
| `frbit environments logs [environment]` | Show environment logs: `--source`, `--limit`, `--since`, `--status`, `--method`, `--follow`. |
| `frbit environments metrics [environment]` | Show the usage summary, or the history of one metric with `--metric <slug>`, `--range` and `--step`. Accepts `--json`. |
| `frbit deployments list` / `get <id>` / `logs <id>` | List deployments, get one, or retrieve its logs (`logs --follow` until the deployment ends). `list` accepts `--page`, `--limit`, `--all`, and `--environment`/`-e` (an ID or a label; default: all deployments). |
| `frbit domains list` / `get <id>` / `delete <id>` / `create <environment> <name>` / `dns <id>` | List, get, delete, create domains, or show their DNS record. `list` accepts `--page`, `--limit`, `--all`, and repeatable `--id`. |
| `frbit jobs list [environment]` / `get <id>` / `create <environment>` / `update <id>` / `delete <id>` | Manage workers and cron jobs; `list` shows the jobs-plan limit, `delete` needs `--confirm <id>` without a terminal. |
| `frbit backups list [environment]` / `get <backup>` / `create <environment>` | List, get, or request backups. `list` accepts `--limit`, `--all`, and `--json`. |
| `frbit backups download <backup>` | Download the files or the database of a backup through a signed link. Accepts `--type files\|database`, `--output` (file, folder, or `-` for stdout), `--force`. |
| `frbit backups restore <backup>` | Restore a backup, overwriting the live data of its environment; needs `--confirm <environment-id>` without a terminal. Accepts `--json`. |
| `frbit mysql get [environment]` / `rotate-password <environment>` | Show host, port, user and database (never the password), or generate a new password; `rotate-password` needs `--confirm <environment>` without a terminal. |
| `frbit db pull [environment]` | Dump the database of an environment to `<environment>.sql.gz` over SSH. Accepts `--output`, `--stdout` (plain SQL), `--force`. |
| `frbit db push <file\|-> [environment]` | Replace the database of an environment with a local dump over SSH, after a backup dump on the environment. Accepts `--confirm`. |
| `frbit files pull [path]` / `push [path]` | Sync files between `/data/www/` on an environment and the current folder with rsync over SSH. Accept `--environment`/`-e`, `--dry-run`, `--delete`, `--confirm` (push, pull without a path, pull with `--delete`). No sync touches `.frbit-*` or default dump and backup files; a whole-folder sync also leaves `.git/`, `.fortrabbit/`, `.env` and `.env.*` alone. |
| `frbit people list` / `get <id>` | List or get people. `list` accepts `--limit`, `--all`, and repeatable `--id`. |
| `frbit teams list` / `get <id>` / `delete <id>` | List, get, or delete teams. `list` accepts `--limit` and `--all`. |
| `frbit payment-methods list` / `get <id>` / `delete <id>` | List, get, or delete payment methods. `list` accepts `--limit` and `--all`. |
| `frbit regions list` / `presets list` / `software list` | List regions, software presets, or software with versions. |
| `frbit plans list` | List component plans with sizes and prices. Accepts `--region`, `--currency`, and `--component`. |
| `frbit php-versions list` / `php-extensions list` / `php-settings list` / `node-versions list` | List the available PHP versions, PHP extensions, php.ini directives with accepted values, or Node.js versions. |
| `frbit git repositories` | List repositories of the connected Git account. |
| `frbit git branches <owner/repo>` | List the branches of a repository. |
| `frbit link [environment]` | Show the environment of the checkout, or link it: confirm the connected repository, or write `.fortrabbit/environments.yml` at the git top level. Accepts `--label`, `--json` (without an environment). |
| `frbit uninstall` | Remove the agent setup, the Claude Code plugin, the sign-in, the config and cache folders and the binary after confirmation. Accepts `--yes`. |
| `frbit setup agent` | Register the remote MCP server and install skills for detected agents. Accepts repeatable `--agent`; `--remove` undoes it and signs out. |
| `frbit mcp install` | Register or update the named fortrabbit MCP entry. Accepts repeatable `--agent`. |
| `frbit mcp list` | List the fortrabbit MCP status, URL, and agent configuration paths. |
| `frbit mcp remove` | List and remove the named fortrabbit MCP entry after confirmation. Accepts `--yes`. |
| `frbit skills install` | Install the latest fortrabbit agent skills. Accepts repeatable `--agent` and `--project`. |
| `frbit skills list` | List installed skills, versions, scopes, and target paths. |
| `frbit skills update` | Update installed skills, or report that the installed version is current. |
| `frbit skills remove` | List and remove installed skills after confirmation. Accepts `--yes`. |
| `frbit version` | Print the CLI version, source commit, and build date. |

Global options:

| Option | Description |
| --- | --- |
| `--profile <name>` | Stored credential profile to use. Defaults to `default`. |

## Environment variables

| Variable | Description |
| --- | --- |
| `FRBIT_TOKEN` | Public API token for the current command. Overrides a stored credential. |
| `FORTRABBIT_ENVIRONMENT` | Environment of the current checkout, after an explicit argument. Set by the platform on every environment. |

## Troubleshooting

If a command reports that no API token is available, set `FRBIT_TOKEN` for that command or run `frbit auth login`.

If authentication fails, confirm that the token is active and has access to the expected apps, then run `frbit auth status` to test the exact profile and host in use.
