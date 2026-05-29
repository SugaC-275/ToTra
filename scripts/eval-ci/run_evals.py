"""
CI script: loads eval suite configs from .totra-evals/*.json,
triggers runs via ToTra API, waits for completion, fails if score_pct < threshold.

Config file format (.totra-evals/my-suite.json):
{
  "suite_id": "uuid-of-suite",
  "model": "gpt-4o-mini",
  "min_score_pct": 80,
  "prompt_version": null
}
"""
import os
import json
import sys
import time
from pathlib import Path

import requests


class ToTra:
    """Minimal ToTra API client for CI use."""

    def __init__(self, api_key: str, base_url: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.session = requests.Session()
        self.session.headers.update({
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        })

    def run_eval(self, suite_id: str, model: str, prompt_version=None) -> dict:
        payload = {"model": model}
        if prompt_version is not None:
            payload["prompt_version"] = prompt_version
        resp = self.session.post(
            f"{self.base_url}/v1/evals/suites/{suite_id}/run",
            json=payload,
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json()

    def get_run(self, run_id: str) -> dict:
        resp = self.session.get(
            f"{self.base_url}/v1/evals/runs/{run_id}",
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json()

    def wait_for_run(self, run_id: str, poll_interval: int = 5, timeout: int = 600) -> dict:
        deadline = time.time() + timeout
        while time.time() < deadline:
            result = self.get_run(run_id)
            status = result.get("status", "")
            if status in ("completed", "failed"):
                return result
            time.sleep(poll_interval)
        raise TimeoutError(f"Run {run_id} did not complete within {timeout}s")


def main() -> None:
    api_key = os.environ.get("TOTRA_API_KEY", "")
    base_url = os.environ.get("TOTRA_BASE_URL", "")
    if not api_key or not base_url:
        print("ERROR: TOTRA_API_KEY and TOTRA_BASE_URL must be set.")
        sys.exit(1)

    client = ToTra(api_key, base_url)

    eval_dir = Path(".totra-evals")
    configs = list(eval_dir.glob("*.json")) if eval_dir.exists() else []

    if not configs:
        print("No eval configs found in .totra-evals/. Skipping.")
        sys.exit(0)

    failed = []
    for cfg_path in sorted(configs):
        cfg = json.loads(cfg_path.read_text())
        suite_id = cfg["suite_id"]
        model = cfg["model"]
        prompt_version = cfg.get("prompt_version")
        threshold = cfg.get("min_score_pct", 80)

        print(f"Running suite {suite_id} with model {model}...")
        try:
            run = client.run_eval(suite_id, model, prompt_version)
            result = client.wait_for_run(run["run_id"])
        except Exception as exc:
            print(f"  ERROR: {exc}")
            failed.append(cfg_path.name)
            continue

        score = result.get("score_pct") or 0.0
        status = "PASS" if score >= threshold else "FAIL"
        print(f"  {status}: score={score:.1f}% (threshold={threshold}%)")

        if score < threshold:
            failed.append(cfg_path.name)

    if failed:
        print(f"\nFailed eval suites: {', '.join(failed)}")
        sys.exit(1)

    print("\nAll eval suites passed.")


if __name__ == "__main__":
    main()
