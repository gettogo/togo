#!/usr/bin/env python3
"""Convert a passed engine replay test attachment to an app matrix proof."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import tempfile

TEST = "EngineReplayLayoutTests/renderedConversationSurvivesHelperRestart()"
ATTACHMENT = re.compile(r"engine-replay-proof(?:_\d+_[0-9A-Fa-f-]{36})?\.json")


def xcresult(*args):
    return json.loads(subprocess.check_output(["xcrun", "xcresulttool", *args]))


def matching_nodes(nodes):
    for node in nodes:
        if node.get("nodeIdentifier") == TEST:
            yield node
        yield from matching_nodes(node.get("children", []))


def proof(result, revision):
    report = xcresult("get", "test-results", "tests", "--path", str(result))
    cases = list(matching_nodes(report["testNodes"]))
    if len(cases) != 1 or cases[0].get("result") != "Passed":
        raise ValueError("The engine replay test must have exactly one passing verdict")
    with tempfile.TemporaryDirectory() as directory:
        subprocess.run(
            ["xcrun", "xcresulttool", "export", "attachments", "--path", str(result),
             "--test-id", TEST, "--output-path", directory],
            check=True, stdout=subprocess.DEVNULL,
        )
        root = Path(directory)
        manifest = json.loads((root / "manifest.json").read_bytes())
        attachments = [a for t in manifest for a in t["attachments"]
                       if ATTACHMENT.fullmatch(a["suggestedHumanReadableName"])]
        if len(attachments) != 1 or attachments[0]["isAssociatedWithFailure"]:
            raise ValueError("Expected one successful engine replay evidence attachment")
        name = attachments[0]["exportedFileName"]
        if Path(name).name != name:
            raise ValueError("Unsafe attachment filename")
        evidence = json.loads((root / name).read_bytes())
    if not re.fullmatch(r"[0-9a-f]{40}", evidence["helperRevision"]):
        raise ValueError("Expected a full helper revision")
    if not evidence["scenarioId"].strip():
        raise ValueError("Missing scenario ID")
    if not re.fullmatch(r"[a-z0-9-]+/app/[a-z0-9-]+:conversation:chat:iPhone17:default", evidence["key"]):
        raise ValueError("Unexpected app matrix cell")
    return [{"key": evidence["key"], "revision": revision, "tier": "replay-ui", "result": "passed"}]


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("xcresult", type=Path)
    parser.add_argument("app_revision", help="Full app revision passed to the test job")
    args = parser.parse_args()
    try:
        if not re.fullmatch(r"[0-9a-f]{40}", args.app_revision):
            raise ValueError("Expected a full app revision")
        print(json.dumps(proof(args.xcresult, args.app_revision)))
    except (KeyError, OSError, TypeError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"{error}\n")
