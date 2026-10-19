package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// Contacts are the numbers shown next to a blood pressure crisis. They come from
// the environment (.env) rather than a settings form: they are set once at install
// time and must never be one more thing to configure in the middle of the night.
type Contacts struct {
	EmergencyNumber string `json:"emergency_number,omitempty"`
	DoctorName      string `json:"doctor_name,omitempty"`
	DoctorPhone     string `json:"doctor_phone,omitempty"`
}

func ContactsFromEnv() Contacts {
	return Contacts{
		EmergencyNumber: strings.TrimSpace(os.Getenv("EMERGENCY_NUMBER")),
		DoctorName:      strings.TrimSpace(os.Getenv("DOCTOR_NAME")),
		DoctorPhone:     strings.TrimSpace(os.Getenv("DOCTOR_PHONE")),
	}
}

type ContactsHandler struct {
	Contacts Contacts
}

func (h *ContactsHandler) GetContacts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.Contacts)
}
