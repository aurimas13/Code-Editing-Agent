#!/usr/bin/env python3
"""Print test keys for a local PostgREST, as shell exports.

    eval "$(python3 scripts/local_keys.py <jwt-secret>)"

The keys are HS256 JWTs carrying a role claim, which is what Supabase's
legacy service_role and anon keys are. For local and CI use only.
"""
import base64, hashlib, hmac, json, sys

secret = sys.argv[1].encode()


def b64(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()


def token(role: str) -> str:
    head = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    body = b64(json.dumps({"role": role, "iss": "local-test"}).encode())
    sig = b64(hmac.new(secret, f"{head}.{body}".encode(), hashlib.sha256).digest())
    return f"{head}.{body}.{sig}"


print(f"export SUPABASE_TEST_SECRET_KEY={token('service_role')}")
print(f"export SUPABASE_TEST_PUBLISHABLE_KEY={token('anon')}")
