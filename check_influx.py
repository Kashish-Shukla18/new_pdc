#!/usr/bin/env python3
import requests
import json

headers = {
    "Authorization": "Token my-super-secret-token",
    "Content-Type": "application/vnd.flux"
}

query = 'from(bucket:"synchrophasor") |> range(start: -30m) |> group(columns: ["_measurement"]) |> count()'

resp = requests.post("http://localhost:8087/api/v2/query", headers=headers, data=query)
print(f"Status: {resp.status_code}")
print(f"Response:\n{resp.text[:500]}")
