"""Validate real PostgreSQL-backed handler traffic against OpenAPI 3.1.

The input is produced only by synthetic Go integration fixtures, never live
traffic. Local refs are resolved in memory; no schema or URL is fetched.
"""
import copy
import csv
import io
import json
from pathlib import Path
import re
import sys
from urllib.parse import parse_qs, urlsplit

from jsonschema import Draft202012Validator, FormatChecker, ValidationError
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012
import yaml


class Contract:
    def __init__(self, document):
        self.document = document
        self.registry = Registry().with_resource(
            "urn:amsonia:openapi", Resource(document, specification=DRAFT202012)
        )

    def resolve(self, value):
        while "$ref" in value:
            ref = value["$ref"]
            assert ref.startswith("#/"), "External references are not allowed"
            value = self.document
            for part in ref[2:].split("/"):
                value = value[part.replace("~1", "/").replace("~0", "~")]
        return value

    def validate(self, schema, value):
        # Resolve component references relative to the OpenAPI document, even
        # when the schema itself is embedded in a request/response operation.
        schema = copy.deepcopy(schema)
        def absolute_refs(node):
            if isinstance(node, dict):
                for key, item in node.items():
                    if key == "$ref":
                        assert item.startswith("#/"), "External reference"
                        node[key] = "urn:amsonia:openapi" + item
                    else:
                        absolute_refs(item)
            elif isinstance(node, list):
                for item in node:
                    absolute_refs(item)
        absolute_refs(schema)
        Draft202012Validator(schema, registry=self.registry, format_checker=FormatChecker()).validate(value)

    def check(self, record):
        url = urlsplit(record["url"])
        for template, path in self.document["paths"].items():
            match = re.fullmatch(re.sub(r"\{[^}]+\}", "([^/]+)", template), url.path)
            if match:
                break
        else:
            raise AssertionError("Undocumented path " + url.path)
        operation = path[record["method"].lower()]
        valid = True
        try:
            if "requestBody" in operation:
                request = self.resolve(operation["requestBody"])
                if request.get("required") or record["request_body"] is not None:
                    self.validate(request["content"]["application/json"]["schema"], record["request_body"])
            parameters = path.get("parameters", []) + operation.get("parameters", [])
            path_values = dict(zip(re.findall(r"\{([^}]+)\}", template), match.groups()))
            query = parse_qs(url.query, keep_blank_values=True)
            for parameter in parameters:
                p = self.resolve(parameter)
                values = [path_values[p["name"]]] if p["in"] == "path" else query.get(p["name"], [])
                assert values or not p.get("required"), "Missing parameter " + p["name"]
                for value in values:
                    kind = p["schema"].get("type")
                    if kind == "integer":
                        value = int(value)
                    elif kind == "boolean":
                        assert value in ("true", "false")
                        value = value == "true"
                    self.validate(p["schema"], value)
        except Exception:
            if record["request_valid"]:
                raise
            valid = False
        assert valid == record["request_valid"], "Invalid-request probe was accepted by schema"
        status = str(record["status"])
        assert status in operation["responses"], f"Undocumented status {status}"
        response = self.resolve(operation["responses"][status])
        for name, header in response.get("headers", {}).items():
            value = next((values[0] for key, values in record["headers"].items() if key.lower() == name.lower()), None)
            assert value is not None, f"Missing documented response header {name}"
            self.validate(self.resolve(header)["schema"], value)
        if "content" not in response:
            assert not record["body"], "Body on a bodyless response"
        else:
            media = record["content_type"].split(";")[0]
            assert media in response["content"], f"Undocumented content type {media}"
            if media == "application/json":
                body = json.loads(record["body"])
            elif media == "text/csv":
                body = record["body"]
                rows = list(csv.reader(io.StringIO(body.lstrip("\ufeff")), strict=True))
                assert rows and all(len(row) == len(rows[0]) for row in rows), "Malformed CSV"
            else:
                raise AssertionError("Unsupported contract media type " + media)
            self.validate(response["content"][media]["schema"], body)
        return operation["operationId"]


def main():
    contract = Contract(yaml.safe_load(Path(sys.argv[1]).read_text()))
    records = [json.loads(line) for line in Path(sys.argv[2]).read_text().splitlines()]
    assert records, "No real HTTP traffic captured"
    covered = set()
    failures = []
    for index, record in enumerate(records, 1):
        try:
            covered.add(contract.check(record))
        except Exception as error:
            # Do not print synthetic credentials or secret token bodies.
            detail = f"{error.validator} at /{'/'.join(map(str, error.absolute_path))}" if isinstance(error, ValidationError) else str(error)
            failures.append(f"Record {index}: {record['method']} {urlsplit(record['url']).path}: {type(error).__name__}: {detail}")
    assert not failures, "\n".join(failures)
    methods = {"get", "post", "put", "patch", "delete", "options", "head", "trace"}
    required = {operation["operationId"] for path in contract.document["paths"].values() for method, operation in path.items() if method in methods}
    assert required <= covered, f"Missing operations: {sorted(required - covered)}"
    # Prove the validator rejects drift using the same real login response.
    login = next(r for r in records if r["url"] == "/api/v1/auth/login" and r["status"] == 200)
    for mutation in ("missing-field", "wrong-type", "undocumented-status"):
        bad = copy.deepcopy(login)
        body = json.loads(bad["body"])
        if mutation == "missing-field":
            del body["access_token"]
        elif mutation == "wrong-type":
            body["account"]["system_admin"] = "yes"
        else:
            bad["status"] = 299
        bad["body"] = json.dumps(body)
        try:
            contract.check(bad)
        except Exception:
            continue
        raise AssertionError("Validator accepted " + mutation)
    print(f"Validated {len(records)} real HTTP exchanges across {len(covered)} operations; drift probes rejected.")


if __name__ == "__main__":
    main()
