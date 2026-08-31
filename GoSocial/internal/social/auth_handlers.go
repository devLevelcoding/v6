package social

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"gosocial/internal/auth"
)

// Register handles POST /auth/register {username,password,displayName}.
// On success it also calls profilesvc over gRPC (UpsertProfile), through
// the circuit breaker in internal/profileclient.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Username == "" || len(body.Password) < 6 {
		writeErr(w, http.StatusBadRequest, "username and a password of at least 6 characters are required")
		return
	}
	hash, err := HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash password: "+err.Error())
		return
	}
	cred, err := h.Creds.Create(body.Username, hash)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if body.DisplayName == "" {
		body.DisplayName = body.Username
	}
	if err := h.Store.RegisterUser(cred.ID, cred.Username, body.DisplayName); err != nil {
		writeErr(w, http.StatusInternalServerError, "register user event: "+err.Error())
		return
	}

	grpcStatus := "ok"
	if h.Profiles != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if _, err := h.Profiles.RPCUpsert(ctx, cred.ID, cred.Username, body.DisplayName); err != nil {
			grpcStatus = "profilesvc unavailable: " + err.Error()
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": cred.ID, "username": cred.Username, "displayName": body.DisplayName,
		"profilesvc": grpcStatus,
	})
}

// Login handles POST /auth/login {username,password} -> a JWT access token.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	cred, err := h.Creds.FindByUsername(body.Username)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := CheckPassword(cred.PasswordHash, body.Password); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := auth.GenerateAccessToken(cred.ID, cred.Username, h.Secret)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "generate token: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accessToken": token, "tokenType": "Bearer", "expiresIn": 86400,
		"user": map[string]any{"id": cred.ID, "username": cred.Username},
	})
}
