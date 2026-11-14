"""Small real-inference check, separate from fixture coverage and the full eval suite."""

import http.cookiejar
import json
import pathlib
import sys
import time
import urllib.request
import uuid

base = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8090"
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def call(path, body=None):
    request = urllib.request.Request(
        base + "/api" + path,
        data=json.dumps(body).encode() if body is not None else None,
        headers={"Content-Type": "application/json"},
    )
    with opener.open(request, timeout=100) as response:
        return json.load(response)


bootstrap = call("/bootstrap")
assert bootstrap["model"].startswith("ollama:"), "a fixture is not real inference"
call("/session", {"profile_id": "anton"})
started = time.monotonic()
answer = call("/messages", {"id": "cpu-"+uuid.uuid4().hex, "text": "В какой период чек-листа нужно изучать Grafana?"})
elapsed_ms = round((time.monotonic() - started) * 1000)
assert answer["kind"] != "fixture", answer
assert "месяц" in answer["text"].lower() and "перв" in answer["text"].lower(), answer
assert answer["source_ids"], answer
for source in answer["source_ids"]:
    call("/documents/" + source)
report = {"model": bootstrap["model"], "real_inference": True, "elapsed_ms": elapsed_ms, "answer": answer, "scope": "one real grounded answer via browser API; full quality acceptance is a separate suite"}
pathlib.Path("reports").mkdir(exist_ok=True)
pathlib.Path("reports/model-cpu.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
print(json.dumps(report, ensure_ascii=False))
