# Records consent for the hooks in Hermes' config.yaml, using Hermes' own functions.
# Run with Hermes' Python after `hpk install --yes`, only when you mean to allow them.
# --refresh first revokes hpk's own hooks, so a new hpk binary is approved again
# (Hermes flags "script modified since approval" otherwise).
import sys

from hermes_cli.config import load_config
from agent.shell_hooks import record_consent_from_config, revoke, _hook_specs

cfg = load_config()
if "--refresh" in sys.argv:
    specs, _ = _hook_specs(cfg)
    for cmd in sorted({s.command for s in specs if "/hpk/bin/hpk" in s.command.replace("\\", "/")}):
        print("revoked", revoke(cmd), cmd.rsplit(" ", 1)[-1])
specs = record_consent_from_config(cfg, accept_hooks=True)
print("allowed", len(specs), "hooks")
for s in specs:
    print(" ", s.event, s.command.rsplit(" ", 1)[-1])
