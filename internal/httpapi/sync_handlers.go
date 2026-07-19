package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/aleksey-mu/travel-planner-server/internal/syncstore"
)

func (a *API) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if err := a.store.Bootstrap(userID); err != nil {
		a.log.Error("bootstrap", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "bootstrap failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"rootId": userID})
}

func (a *API) handleQuota(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	u, err := a.users.GetByID(r.Context(), userID)
	if err != nil {
		a.log.Error("quota user", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "quota failed")
		return
	}
	view, err := a.store.BuildQuotaForUser(userID, u.QuotaBytes, a.cfg.DefaultQuotaBytes)
	if err != nil {
		a.log.Error("quota", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "quota failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) handleGetManifest(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	b, err := a.store.GetManifest(userID)
	if err != nil {
		a.log.Error("get manifest", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "read failed")
		return
	}
	writeBytes(w, http.StatusOK, "application/json", b)
}

func (a *API) handlePutManifest(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	body, err := a.readJSONBody(r)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	if err := a.store.PutManifest(userID, body); err != nil {
		a.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGetTravel(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	travelID := r.PathValue("travelId")
	b, err := a.store.GetTravel(userID, travelID)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	writeBytes(w, http.StatusOK, "application/json", b)
}

func (a *API) handlePutTravel(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	travelID := r.PathValue("travelId")
	body, err := a.readJSONBody(r)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	if err := a.store.PutTravel(userID, travelID, body); err != nil {
		a.mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"fileId": travelID})
}

func (a *API) handleDeleteTravel(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	travelID := r.PathValue("travelId")
	if err := a.store.DeleteTravel(userID, travelID); err != nil {
		a.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleGetUserData(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	fileName := r.PathValue("fileName")
	if !syncstore.IsAllowedUserDataFile(fileName) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid user-data file name")
		return
	}
	b, err := a.store.GetUserData(userID, fileName)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	writeBytes(w, http.StatusOK, "application/json", b)
}

func (a *API) handlePutUserData(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	fileName := r.PathValue("fileName")
	if !syncstore.IsAllowedUserDataFile(fileName) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid user-data file name")
		return
	}
	body, err := a.readJSONBody(r)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	if err := a.store.PutUserData(userID, fileName, body); err != nil {
		a.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleEnsureFolder(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	travelID := r.PathValue("travelId")
	itemID := r.PathValue("itemId")
	folderID, err := a.store.EnsureFolder(userID, travelID, itemID)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"folderId": folderID})
}

func (a *API) handlePutFile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	travelID := r.PathValue("travelId")
	itemID := r.PathValue("itemId")
	fileName := r.PathValue("fileName")
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadBytes+1)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "upload exceeds max size")
			return
		}
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body")
		return
	}
	if int64(len(body)) > a.cfg.MaxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "upload exceeds max size")
		return
	}
	fileID, err := a.store.PutFile(userID, travelID, itemID, fileName, body)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"fileId": fileID})
}

func (a *API) handleGetFile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	raw := r.PathValue("fileId")
	rel, err := syncstore.NormalizeFileID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid file id")
		return
	}
	b, ct, err := a.store.GetFile(userID, rel)
	if err != nil {
		a.mapStoreErr(w, err)
		return
	}
	writeBytes(w, http.StatusOK, ct, b)
}

func (a *API) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	raw := r.PathValue("fileId")
	rel, err := syncstore.NormalizeFileID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid file id")
		return
	}
	if err := a.store.DeleteFile(userID, rel); err != nil {
		a.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) readJSONBody(r *http.Request) ([]byte, error) {
	body, err := syncstore.ReadBodyLimited(r.Body, a.cfg.MaxUploadBytes)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (a *API) mapStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, syncstore.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
	case errors.Is(err, syncstore.ErrQuotaExceeded):
		writeError(w, http.StatusRequestEntityTooLarge, "QUOTA_EXCEEDED", "storage quota exceeded")
	case errors.Is(err, syncstore.ErrDiskFull):
		writeError(w, http.StatusInsufficientStorage, "DISK_FULL", "not enough free disk space on the server")
	case errors.Is(err, syncstore.ErrPayloadTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "upload exceeds max size")
	case err != nil && (strings.Contains(err.Error(), "invalid") ||
		strings.Contains(err.Error(), "traversal") ||
		strings.Contains(err.Error(), "json") ||
		strings.Contains(err.Error(), "path")):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	default:
		a.log.Error("store", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}
