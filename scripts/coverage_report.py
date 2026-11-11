"""Combine distinct backend/frontend line reports without averaging metrics."""
import json
from pathlib import Path
import shutil
import re
import subprocess


root = Path(__file__).resolve().parent.parent
records = (root / "coverage/go.lcov").read_text().splitlines()
found = sum(int(line[3:]) for line in records if line.startswith("LF:"))
hit = sum(int(line[3:]) for line in records if line.startswith("LH:"))
assert found > 0, "No Go lines were instrumented"
backend = {"covered": hit, "total": found, "pct": round(hit * 100 / found, 2)}
statement_report = subprocess.check_output(["go", "tool", "cover", "-func=coverage/go.out"], cwd=root, text=True)
statement_percent = float(re.search(r"total:\s+\(statements\)\s+([\d.]+)%", statement_report).group(1))
frontend = json.loads((root / "web/coverage/coverage-summary.json").read_text())["total"]
summary = {
    "backend_lines": backend,
    "backend_statements_pct": statement_percent,
    "frontend_lines": frontend["lines"],
    "frontend_statements": frontend["statements"],
    "frontend_branches": frontend["branches"],
    "frontend_functions": frontend["functions"],
    "method": "Go coverprofile converted to executable-line LCOV by gcov2lcov v1.1.1; frontend Vitest V8. Native Go statement coverage is a separate metric. No cross-language average.",
    "exclusions": "Dependencies and tests. Frontend type-only declarations have no executable statements. No authored runtime file or entrypoint excluded.",
}
destination = root / "reports/coverage"
destination.mkdir(parents=True, exist_ok=True)
shutil.copy(root / "coverage/go.lcov", destination / "backend.lcov")
shutil.copy(root / "web/coverage/lcov.info", destination / "frontend.lcov")
(destination / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n")
print(json.dumps({"backend_lines": backend, "frontend_lines": frontend["lines"]}, ensure_ascii=False))
assert backend["pct"] >= 85, "Backend line coverage must be at least 85%"
assert frontend["lines"]["pct"] >= 85, "Frontend line coverage must be at least 85%"
