# Operations notes

The commands live in the README. This file keeps the reasons behind them, because each of these
was learned from a failure that looked like something else.

## One storage, one writer

`.data/agent.owner` is created exclusively (`wx`) with the pid; a second process errors out
instead of corrupting a live conversation.

A **recycled pid does not block startup**: besides `kill(pid, 0)`, the command line from
`ps -p <pid> -o command=` must look like `node … main.ts`. Before this, a hard kill could leave the
lock pointing at somebody else's process and launchd would retry forever — which looks exactly like
"it did not start after a reboot".

## A broken storage heals itself

Once a session reports `Session failed after a storage error`, only a new process can recover it,
so the bridge exits and lets launchd start a fresh one; at most once per 10 minutes
(`.data/storage-selfheal`), so a truly corrupt database cannot become a restart loop.

## Logs

`logs/agent.log`, `logs/agent.err.log`, `logs/backup.log`. A `[report] <time> <kind>` line is a
failure inside a hook, tool or listener — it never reaches the harness, so it is only visible here.
Rotation keeps 5 MB × 3; `LOG_MAX_BYTES` overrides.

## Backups

`sqlite3 .backup`, then `journal_mode=DELETE` to collapse it to one file, an integrity check, 14
kept. A restore stops the process and waits for it to really exit.

## doctor

`agentctl doctor` judges relative to the previous run: restarts as `+N since last check`, errors
only from the last two days, and untimestamped old log lines counted separately so they cannot keep
posing as a new fault.

## Two launchd jobs, one conversation state

`com.pilot.agent` runs the bridge, `com.pilot.agent.feishu-gw` runs the Feishu connection
(`logs/feishu-gw.log`, `KeepAlive`, 10 s throttle); neither restarts the other. The gateway is
`KeepAlive` because a bot whose gateway died is deaf with no symptom; `agentctl status` is what says
so out loud (`feishu: ok (gateway connected)`).

The plist is a template (`launchd/*.plist.tmpl`) rendered at install time, so a different machine
needs no edits.
