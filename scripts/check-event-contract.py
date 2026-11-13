"""Validate persisted events captured by the Go integration tests."""

import json
import sys
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker


def main():
    root = Path(__file__).resolve().parent.parent
    schema = json.loads((root / "schemas/event.schema.json").read_text())
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema, format_checker=FormatChecker())
    captures = sorted(Path(sys.argv[1]).glob("events-*.json"))
    if not captures:
        sys.exit("No events were captured. Run the integration tests with WEFT_TEST_EVENT_CAPTURE set.")
    count = 0
    kinds = set()
    failures = []
    for path in captures:
        capture = json.loads(path.read_text())
        for event in capture["events"]:
            count += 1
            kinds.add(event["type"])
            for error in validator.iter_errors(event):
                location = ".".join(map(str, error.absolute_path)) or "$"
                failures.append(f"{capture['test']}: {event['type']} at {location}: {error.message}")
    if failures:
        sys.exit("\n".join(failures))
    print(f"Validated {count} events of {len(kinds)} types from {len(captures)} integration scenarios.")


if __name__ == "__main__":
    main()
