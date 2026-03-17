"""Demo app: business logic only, auth delegated to external IAM via OIDC."""

import os
from contextlib import asynccontextmanager
from typing import Annotated

import jwt
from fastapi import Depends, FastAPI, HTTPException, status
from fastapi.security import HTTPBearer
from pydantic import BaseModel

# OIDC_ISSUER_URL: public issuer — must match the `iss` claim in tokens
# OIDC_JWKS_URI:   where to fetch signing keys (internal URL in Docker)
OIDC_ISSUER_URL = os.environ["OIDC_ISSUER_URL"].rstrip("/")
OIDC_JWKS_URI = os.environ["OIDC_JWKS_URI"]
OIDC_AUDIENCE = os.environ.get("OIDC_AUDIENCE", "")
IAM_INTERNAL_URL = os.environ.get("IAM_INTERNAL_URL", "").rstrip("/")  # e.g. http://launch-kit:8080/t/demo
IAM_ADMIN_EMAIL = os.environ.get("IAM_ADMIN_EMAIL", "")
IAM_ADMIN_PASSWORD = os.environ.get("IAM_ADMIN_PASSWORD", "")

jwks_client: jwt.PyJWKClient | None = None
iam_admin_token: str = ""


def _get_iam_admin_token() -> str:
    """Login as IAM admin to get a service token for contact creation."""
    global iam_admin_token
    if iam_admin_token:
        # Quick check: is it still valid?
        try:
            jwt.decode(iam_admin_token, options={"verify_signature": False, "verify_exp": True})
            return iam_admin_token
        except jwt.ExpiredSignatureError:
            pass
    import urllib.request
    import json
    req = urllib.request.Request(
        f"{IAM_INTERNAL_URL}/login",
        data=json.dumps({"email": IAM_ADMIN_EMAIL, "password": IAM_ADMIN_PASSWORD}).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req) as resp:
        data = json.loads(resp.read())
    iam_admin_token = data["access_token"]
    return iam_admin_token


@asynccontextmanager
async def lifespan(app: FastAPI):
    global jwks_client
    jwks_client = jwt.PyJWKClient(OIDC_JWKS_URI, cache_jwk_set=True, lifespan=300)
    yield


app = FastAPI(title="Launch Kit Demo", lifespan=lifespan)
security = HTTPBearer()

# In-memory store (demo only)
notes_db: dict[str, list[dict]] = {}


class SubscribeRequest(BaseModel):
    email: str
    name: str


class ActivateRequest(BaseModel):
    password: str


class NoteCreate(BaseModel):
    title: str
    body: str = ""


def get_current_user(creds=Depends(security)) -> dict:
    """Validate JWT: signature via JWKS, issuer, audience, expiry."""
    try:
        signing_key = jwks_client.get_signing_key_from_jwt(creds.credentials)
        payload = jwt.decode(
            creds.credentials,
            signing_key.key,
            algorithms=["RS256"],
            issuer=OIDC_ISSUER_URL,
            audience=OIDC_AUDIENCE or None,
            options={"verify_aud": bool(OIDC_AUDIENCE)},
        )
        return {"sub": payload["sub"], "email": payload.get("email", payload["sub"])}
    except jwt.InvalidTokenError as e:
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, str(e))


# --- Public ---

@app.get("/api/health")
def health():
    return {"status": "ok"}


@app.get("/api/stats")
def public_stats():
    """Unsecured — aggregate stats."""
    total_notes = sum(len(n) for n in notes_db.values())
    return {"users": len(notes_db), "notes": total_notes}


@app.post("/api/subscribe", status_code=201)
def subscribe(req: SubscribeRequest):
    """Landing page signup — creates a contact in IAM (no password yet)."""
    import urllib.request
    import json
    token = _get_iam_admin_token()
    body = json.dumps({"email": req.email, "name": req.name}).encode()
    http_req = urllib.request.Request(
        f"{IAM_INTERNAL_URL}/admin/contacts",
        data=body,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(http_req) as resp:
            contact = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        detail = json.loads(e.read()).get("error_description", "Subscription failed")
        raise HTTPException(e.code, detail)
    return {
        "status": "subscribed",
        "email": contact["email"],
        "invite_token": contact.get("invite_token", ""),  # in production, sent via email
    }


@app.post("/api/activate/{invite_token}")
def activate(invite_token: str, req: ActivateRequest):
    """Activate invite — sets password, creates user account."""
    import urllib.request
    import json
    body = json.dumps({"password": req.password}).encode()
    http_req = urllib.request.Request(
        f"{IAM_INTERNAL_URL}/activate/{invite_token}",
        data=body,
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(http_req) as resp:
            result = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        detail = json.loads(e.read()).get("error_description", "Activation failed")
        raise HTTPException(e.code, detail)
    return result


# --- Protected (any valid IAM token) ---

@app.get("/api/dashboard")
def dashboard(user: Annotated[dict, Depends(get_current_user)]):
    user_notes = notes_db.get(user["sub"], [])
    return {
        "email": user["email"],
        "your_notes": len(user_notes),
        "total_users": len(notes_db),
        "total_notes": sum(len(n) for n in notes_db.values()),
    }


@app.get("/api/notes")
def list_notes(user: Annotated[dict, Depends(get_current_user)]):
    return notes_db.get(user["sub"], [])


@app.post("/api/notes", status_code=201)
def create_note(note: NoteCreate, user: Annotated[dict, Depends(get_current_user)]):
    entries = notes_db.setdefault(user["sub"], [])
    entry = {"id": len(entries) + 1, "title": note.title, "body": note.body}
    entries.append(entry)
    return entry


@app.delete("/api/notes/{note_id}")
def delete_note(note_id: int, user: Annotated[dict, Depends(get_current_user)]):
    entries = notes_db.get(user["sub"], [])
    for i, n in enumerate(entries):
        if n["id"] == note_id:
            entries.pop(i)
            return {"status": "deleted"}
    raise HTTPException(status.HTTP_404_NOT_FOUND)
