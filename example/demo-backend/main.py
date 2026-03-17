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

jwks_client: jwt.PyJWKClient | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global jwks_client
    jwks_client = jwt.PyJWKClient(OIDC_JWKS_URI, cache_jwk_set=True, lifespan=300)
    yield


app = FastAPI(title="Launch Kit Demo", lifespan=lifespan)
security = HTTPBearer()

# In-memory store (demo only)
notes_db: dict[str, list[dict]] = {}


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
