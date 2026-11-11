"""Walk the real HTTP demo boundary without a browser or external dependencies."""
import http.cookiejar
import json
import os
import sys
import urllib.error
import urllib.request


base = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8090").rstrip("/")
profile_id = os.environ.get("SMOKE_PROFILE", "anton")
operator = os.environ.get("DEMO_OPERATOR_TOKEN", "local-demo-owner")
jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def call(method, path, body=None, expected=200, owner=False):
    headers = {"Content-Type": "application/json", "Origin": base}
    if owner:
        headers["X-Demo-Token"] = operator
    request = urllib.request.Request(
        base + "/api" + path,
        data=None if body is None else json.dumps(body).encode(),
        headers=headers,
        method=method,
    )
    try:
        response = client.open(request, timeout=15)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        result = json.load(response)
        assert response.status == expected, (method, path, response.status, result)
        return result


call("GET", "/health")
call("POST", "/session", {"profile_id": profile_id})
state = call("GET", "/state")
profile = state["profile"]
assert profile["id"] == profile_id
assert all(doc["scope"] != "mentor" for doc in state["documents"])
assert all(not doc["team"] or doc["team"] == profile["team"] for doc in state["documents"])
other_team = "delivery" if profile["team"] == "catalog" else "catalog"
call("GET", f"/documents/{other_team}-guide", expected=404)
call("GET", f"/documents/{profile['team']}-mentor-private", expected=404)
call("POST", "/session", {"profile_id": profile_id, "role": "mentor"}, expected=400)
call("POST", "/tasks/space-access/complete", {}, expected=403)

access = next((op for op in reversed(state["operations"]) if op["kind"] == "access" and op["status"] not in ["rejected", "revoked"]), None)
if access is None:
    prepared = call("POST", "/actions", {"kind": "access", "reason": "Изучить руководство команды и первую задачу"})
    assert not any(op["key"] == prepared["id"] for op in call("GET", "/state")["operations"])
    access = call("POST", f"/actions/{prepared['id']}/confirm", {})
    repeated = call("POST", f"/actions/{prepared['id']}/confirm", {})
    assert repeated["id"] == access["id"]
assert sum(op["key"] == access["key"] for op in call("GET", "/state")["operations"]) == 1

if access["status"] == "pending":
    call("POST", "/demo/events", {"key": access["key"], "status": "approved"}, expected=403)
    call("POST", "/demo/events", {"key": access["key"], "status": "approved"}, owner=True)
    access["status"] = "approved"
if access["status"] == "approved":
    call("GET", f"/documents/{profile['team']}-guide", expected=404)
    call("POST", "/demo/events", {"key": access["key"], "status": "granted"}, owner=True)

document = call("GET", f"/documents/{profile['team']}-guide")
assert document["body"] and document["synthetic"]
call("POST", "/tasks/read-guide/complete", {})
state = call("GET", "/state")
assert next(task for task in state["tasks"] if task["id"] == "space-access")["status"] == "completed"
issue = next((op for op in state["operations"] if op["kind"] == "issue"), None)
if issue is None:
    prepared = call("POST", "/actions", {"kind": "issue", "reason": "Первая учебная задача после руководства"})
    issue = call("POST", f"/actions/{prepared['id']}/confirm", {})
assert issue["id"] == profile["project"] + "-101"
call("POST", "/draft", {"question": "Хочу уточнить критерии первой задачи"})
restored = call("POST", "/refresh", {})
assert restored["draft"] and len(restored["operations"]) >= 2
assert next(task for task in restored["tasks"] if task["id"] == "first-issue")["status"] == "completed"
print(json.dumps({"status": "passed", "profile": profile_id, "access": access["id"], "issue": issue["id"], "model": restored["model"], "simulation": True}, ensure_ascii=False))
