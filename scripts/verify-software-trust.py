#!/usr/bin/env python3
"""Fail-closed bootstrap verifier for SwipeNode software trust metadata."""

import argparse
import base64
import datetime as dt
import hashlib
import json
import os
import re
import struct
import subprocess
import sys
import tempfile

SCHEMA = "swipenode.trust-metadata.v1"
PURPOSE = "software_release"
STABLE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$")
VERSION = re.compile(r"^v?[0-9]+(?:\.[0-9]+){1,3}$")


def fail(message):
    raise ValueError(message)


def strict_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            fail("duplicate JSON field")
        result[key] = value
    return result


def exact_fields(value, required, optional=()):
    if not isinstance(value, dict) or not set(required).issubset(value) or not set(value).issubset(set(required) | set(optional)):
        fail("unexpected trust metadata fields")


def public_key(value):
    try:
        decoded = base64.b64decode(value, validate=True)
    except Exception as error:
        raise ValueError("invalid Ed25519 public key") from error
    if len(decoded) != 32:
        fail("invalid Ed25519 public key")
    return decoded


def key_id(raw):
    return "ed25519:" + hashlib.sha256(raw).hexdigest()[:24]


def timestamp(value):
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except (AttributeError, ValueError) as error:
        raise ValueError("invalid trust metadata timestamp") from error
    if parsed.tzinfo is None:
        fail("trust metadata timestamp requires a timezone")
    return parsed.astimezone(dt.timezone.utc)


def version_parts(value):
    if not isinstance(value, str) or not VERSION.fullmatch(value):
        fail("version must contain 2-4 numeric components")
    return tuple(int(part) for part in value.removeprefix("v").split("."))


def compare_versions(left, right):
    a, b = version_parts(left), version_parts(right)
    size = max(len(a), len(b))
    a, b = a + (0,) * (size - len(a)), b + (0,) * (size - len(b))
    return (a > b) - (a < b)


def permits(key, at):
    if at < timestamp(key["active_from"]):
        return False
    if key.get("revoked_at"):
        return False
    return not key.get("retired_at") or at < timestamp(key["retired_at"])


def openssh_key(raw):
    algorithm = b"ssh-ed25519"
    wire = struct.pack(">I", len(algorithm)) + algorithm + struct.pack(">I", len(raw)) + raw
    return "ssh-ed25519 " + base64.b64encode(wire).decode("ascii")


def signing_payload(document):
    keys = []
    optional = ("retired_at", "revoked_at", "revocation_reason", "successor_key_id", "usages")
    for source in sorted(document["keys"], key=lambda item: item["id"]):
        item = {
            "id": source["id"],
            "algorithm": source["algorithm"],
            "public_key": source["public_key"],
            "created_at": source["created_at"],
            "active_from": source["active_from"],
        }
        for name in optional[:-1]:
            if source.get(name):
                item[name] = source[name]
        item["purpose"] = source["purpose"]
        if source.get("usages"):
            item["usages"] = source["usages"]
        keys.append(item)
    minimum = {}
    if document["minimum_versions"].get("software"):
        minimum["software"] = document["minimum_versions"]["software"]
    if document["minimum_versions"].get("packs"):
        minimum["packs"] = dict(sorted(document["minimum_versions"]["packs"].items()))
    payload = {
        "schema_version": document["schema_version"],
        "purpose": document["purpose"],
        "sequence": document["sequence"],
        "issued_at": document["issued_at"],
        "keys": keys,
        "minimum_versions": minimum,
    }
    encoded = json.dumps(payload, ensure_ascii=False, indent=2, separators=(",", ": ")) + "\n"
    if any(ord(character) > 127 for character in encoded):
        fail("non-ASCII v1 trust metadata is not supported by the bootstrap verifier")
    return encoded.encode("utf-8")


def verify_signature(root, payload, signature):
    spki = bytes.fromhex("302a300506032b6570032100") + root
    try:
        signature_bytes = base64.b64decode(signature, validate=True)
    except Exception as error:
        raise ValueError("invalid trust metadata signature encoding") from error
    if len(signature_bytes) != 64:
        fail("invalid trust metadata signature encoding")
    with tempfile.TemporaryDirectory(prefix="swipenode-trust-") as directory:
        paths = [os.path.join(directory, name) for name in ("root.der", "payload", "signature")]
        for path, data in zip(paths, (spki, payload, signature_bytes)):
            with open(path, "wb") as output:
                output.write(data)
            os.chmod(path, 0o600)
        result = subprocess.run(
            ["openssl", "pkeyutl", "-verify", "-pubin", "-inkey", paths[0], "-keyform", "DER", "-rawin", "-in", paths[1], "-sigfile", paths[2]],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
    if result.returncode != 0:
        fail("trust metadata signature is invalid")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--metadata", required=True)
    parser.add_argument("--trust-root", required=True)
    parser.add_argument("--release-key", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--allowed-signers", required=True)
    args = parser.parse_args()

    for path in (args.metadata, args.trust_root, args.release_key):
        if not os.path.isfile(path) or os.path.islink(path):
            fail("trust input must be a regular non-symlink file")
    with open(args.metadata, "r", encoding="utf-8") as source:
        document = json.load(source, object_pairs_hook=strict_object)
    exact_fields(document, ("schema_version", "purpose", "sequence", "issued_at", "keys", "minimum_versions", "signature"))
    if document["schema_version"] != SCHEMA or document["purpose"] != PURPOSE or not isinstance(document["sequence"], int) or isinstance(document["sequence"], bool) or document["sequence"] < 1:
        fail("invalid software trust metadata identity")
    if not isinstance(document["keys"], list) or not document["keys"]:
        fail("software trust metadata has no keys")
    exact_fields(document["minimum_versions"], (), ("software", "packs"))
    minimum_software = document["minimum_versions"].get("software", "")
    minimum_packs = document["minimum_versions"].get("packs", {})
    if minimum_software:
        version_parts(minimum_software)
    if not isinstance(minimum_packs, dict) or any(not isinstance(name, str) or not isinstance(value, str) for name, value in minimum_packs.items()):
        fail("invalid signed minimum versions")
    for value in minimum_packs.values():
        version_parts(value)
    exact_fields(document["signature"], ("algorithm", "key_id", "value"))
    now = dt.datetime.now(dt.timezone.utc)
    if timestamp(document["issued_at"]) > now + dt.timedelta(minutes=5):
        fail("trust metadata issuance is in the future")

    seen = {}
    required = ("id", "algorithm", "public_key", "created_at", "active_from", "purpose")
    optional = ("retired_at", "revoked_at", "revocation_reason", "successor_key_id", "usages")
    for key in document["keys"]:
        exact_fields(key, required, optional)
        raw = public_key(key["public_key"])
        if not isinstance(key["id"], str) or not STABLE_ID.fullmatch(key["id"]) or key["id"] != key_id(raw) or key["algorithm"] != "Ed25519" or key["purpose"] != PURPOSE or key["id"] in seen:
            fail("invalid or duplicate software trust key")
        if timestamp(key["active_from"]) < timestamp(key["created_at"]):
            fail("invalid trust key lifecycle timestamps")
        if key.get("retired_at") and timestamp(key["retired_at"]) < timestamp(key["active_from"]):
            fail("invalid retirement timestamp")
        if bool(key.get("revoked_at")) != bool(key.get("revocation_reason")):
            fail("revocation requires timestamp and reason")
        if key.get("revoked_at"):
            timestamp(key["revoked_at"])
        usages = key.get("usages", [])
        if not isinstance(usages, list) or any(not isinstance(item, str) for item in usages) or len(usages) != len(set(usages)) or any(item not in ("trust_metadata", "content_signing") for item in usages):
            fail("invalid trust key usage")
        seen[key["id"]] = (key, raw)
    if any(key.get("successor_key_id") not in seen or key.get("successor_key_id") == key["id"] for key in document["keys"] if key.get("successor_key_id")):
        fail("invalid successor key")

    with open(args.trust_root, "r", encoding="ascii") as source:
        root = public_key(source.read().strip())
    root_id = key_id(root)
    signature = document["signature"]
    if signature["algorithm"] != "Ed25519" or signature["key_id"] != root_id:
        fail("trust metadata signature identity is invalid")
    root_entry = seen.get(root_id)
    if not root_entry or root_entry[1] != root or (root_entry[0].get("usages") and "trust_metadata" not in root_entry[0]["usages"]):
        fail("bootstrap signer is not declared for software trust metadata")
    verify_signature(root, signing_payload(document), signature["value"])

    minimum = document["minimum_versions"].get("software", "")
    if minimum and compare_versions(args.version, minimum) < 0:
        fail("requested version is below the signed software minimum")
    authorized = []
    for key, raw in seen.values():
        usages = key.get("usages", [])
        if (not usages or "content_signing" in usages) and permits(key, now):
            authorized.append((key["id"], openssh_key(raw)))
    if not authorized:
        fail("no active software release key authorizes the requested version")

    with open(args.release_key, "r", encoding="ascii") as source:
        fields = source.read().strip().split()
    if len(fields) < 2 or not any(fields[0] == key.split()[0] and fields[1] == key.split()[1] for _, key in authorized):
        fail("published software release key is not authorized by signed metadata")
    with open(args.allowed_signers, "x", encoding="ascii") as output:
        for _, key in authorized:
            output.write("swipenode-release " + key + "\n")
    os.chmod(args.allowed_signers, 0o600)
    print(json.dumps({"schema_version": SCHEMA, "sequence": document["sequence"], "authorized_key_ids": [item[0] for item in authorized]}, separators=(",", ":")))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(f"software trust verification failed: {error}", file=sys.stderr)
        sys.exit(1)
