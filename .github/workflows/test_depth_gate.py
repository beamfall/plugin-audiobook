#!/usr/bin/env python3
"""Enforce Audiobook per-package coverage and CI depth contracts."""

from __future__ import annotations

import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "conformance.yml"
GATE = ROOT / "script" / "gate.sh"
FLOORS = ROOT / "testing" / "coverage-floor.json"
FUZZ_TARGETS = {"FuzzParseAudiobookshelfExport", "FuzzParseFilename"}
TOTAL_RE = re.compile(r"^total:\s+\(statements\)\s+([0-9]+(?:\.[0-9]+)?)%$")


class GateError(RuntimeError):
    pass


def run(args: list[str]) -> str:
    result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, check=False)
    if result.returncode:
        raise GateError(f"{' '.join(args)} failed\n{result.stdout}{result.stderr}")
    return result.stdout


def load_floors() -> dict[str, float]:
    data = json.loads(FLOORS.read_text(encoding="utf-8"))
    packages = data.get("packages")
    if data.get("schema_version") != 1 or not isinstance(packages, dict) or not packages:
        raise GateError("coverage-floor.json requires schema_version 1 and package floors")
    floors: dict[str, float] = {}
    for package, raw_floor in packages.items():
        if not isinstance(package, str) or not package or isinstance(raw_floor, bool) or not isinstance(raw_floor, (int, float)):
            raise GateError("coverage floors must map package strings to numbers")
        floor = float(raw_floor)
        if not 0 < floor <= 100:
            raise GateError(f"invalid coverage floor for {package}: {floor}")
        floors[package] = floor
    return floors


def coverage_errors(
    floors: dict[str, float], discovered: set[str], measured: dict[str, float]
) -> list[str]:
    errors = [f"package has no floor: {package}" for package in sorted(discovered - floors.keys())]
    errors += [f"floor targets missing package: {package}" for package in sorted(floors.keys() - discovered)]
    errors += [
        f"{package} coverage {measured.get(package, 0):.1f}% is below {floors[package]:.1f}%"
        for package in sorted(discovered & floors.keys())
        if measured.get(package, 0) < floors[package]
    ]
    return errors


def fuzz_target_errors(discovered: set[str]) -> list[str]:
    return [
        *(f"required fuzz target missing: {target}" for target in sorted(FUZZ_TARGETS - discovered)),
        *(f"unregistered fuzz target: {target}" for target in sorted(discovered - FUZZ_TARGETS)),
    ]


def job(text: str, name: str) -> str:
    match = re.search(rf"^  {re.escape(name)}:\s*$\n(.*?)(?=^  [A-Za-z][A-Za-z0-9-]*:\s*$|\Z)", text, re.MULTILINE | re.DOTALL)
    if not match:
        raise GateError(f"workflow job missing: {name}")
    return match.group(1)


def case_arm(text: str, name: str) -> str:
    match = re.search(rf"^  {re.escape(name)}\)\s*$\n(.*?)(?=^  (?:[A-Za-z][A-Za-z0-9-]*|\*)\)\s*$|\Z)", text, re.MULTILINE | re.DOTALL)
    if not match:
        raise GateError(f"gate case arm missing: {name}")
    return match.group(1)


def validate_contracts(workflow: str, gate: str) -> None:
    fast = job(workflow, "fast-conformance")
    race = job(workflow, "race")
    aggregate = job(workflow, "conformance")
    fuzz = job(workflow, "fuzz")
    race_arm = case_arm(gate, "race")
    required = {
        "fast gate execution": (fast, "run: python3 .github/workflows/test_depth_gate.py check"),
        "race execution": (race, "run: script/gate.sh race"),
        "race dependency": (aggregate, "needs: [fast-conformance, race]"),
        "always-run aggregate": (aggregate, "if: always()"),
        "fast result assertion": (aggregate, 'test "$FAST_RESULT" = success'),
        "race result assertion": (aggregate, 'test "$RACE_RESULT" = success'),
        "scheduled fuzz": (fuzz, "github.event_name == 'schedule'"),
        "bounded fuzz execution": (fuzz, "run: script/gate.sh fuzz"),
        "fuzz budget": (fuzz, "FUZZ_TIME: 20s"),
        "local coverage": (gate, "python3 .github/workflows/test_depth_gate.py check"),
        "local race": (race_arm, "go test -race -count=1 ./..."),
        "runtime fuzz discovery": (gate, "go test -list '^Fuzz[A-Za-z0-9_]*$' ."),
    }
    missing = [name for name, (text, needle) in required.items() if needle not in text]
    missing += [f"registered target {target}" for target in FUZZ_TARGETS if target not in gate]
    if missing:
        raise GateError("test-depth contract missing: " + ", ".join(sorted(missing)))


def contract_proofs(workflow: str, gate: str) -> None:
    planted = coverage_errors({"one": 80, "two": 80}, {"one", "two"}, {"one": 100, "two": 60})
    if not any("two coverage" in error for error in planted):
        raise GateError("aggregate coverage masking proof did not fail")
    print("proof: planted aggregate coverage masking RED")
    mutated = gate.replace("go test -race -count=1 ./...", "true", 1)
    try:
        validate_contracts(workflow, mutated)
    except GateError:
        pass
    else:
        raise GateError("missing race execution proof did not fail")
    print("proof: missing race execution RED")
    if not fuzz_target_errors(FUZZ_TARGETS - {"FuzzParseFilename"}):
        raise GateError("required fuzz target discovery proof did not fail")
    print("proof: required fuzz target discovery RED")


def contracts() -> None:
    workflow = WORKFLOW.read_text(encoding="utf-8")
    gate = GATE.read_text(encoding="utf-8")
    validate_contracts(workflow, gate)
    contract_proofs(workflow, gate)
    listed = run(["go", "test", "-list", r"^Fuzz[A-Za-z0-9_]*$", "."])
    discovered = set(re.findall(r"^Fuzz[A-Za-z0-9_]*$", listed, re.MULTILINE))
    errors = fuzz_target_errors(discovered)
    if errors:
        raise GateError("; ".join(errors))
    print("test-depth contracts: PASS")


def parse_total(output: str, package: str) -> float:
    for line in output.splitlines():
        match = TOTAL_RE.match(line.strip())
        if match:
            return float(match.group(1))
    raise GateError(f"cannot parse coverage total for {package}")


def coverage() -> None:
    floors = load_floors()
    discovered = {line for line in run(["go", "list", "./..."]).splitlines() if line}
    if not discovered:
        raise GateError("go list ./... discovered no packages")
    measured: dict[str, float] = {}
    with tempfile.TemporaryDirectory(prefix="audiobook-coverage-") as temp_dir:
        for index, package in enumerate(sorted(discovered)):
            profile = Path(temp_dir) / f"package-{index}.out"
            sys.stdout.write(run(["go", "test", "-count=1", "-covermode=set", f"-coverprofile={profile}", package]))
            measured[package] = parse_total(run(["go", "tool", "cover", f"-func={profile}"]), package)
    errors = coverage_errors(floors, discovered, measured)
    if errors:
        raise GateError("; ".join(errors))
    for package in sorted(discovered):
        print(f"coverage: {package} {measured[package]:.1f}% >= {floors[package]:.1f}%")


def main() -> int:
    try:
        command = sys.argv[1] if len(sys.argv) == 2 else "check"
        if command in {"contracts", "check"}:
            contracts()
        if command in {"coverage", "check"}:
            coverage()
        if command not in {"contracts", "coverage", "check"}:
            raise GateError("usage: test_depth_gate.py [contracts|coverage|check]")
    except (GateError, OSError, ValueError, json.JSONDecodeError) as error:
        print(f"test-depth gate: FAIL: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
