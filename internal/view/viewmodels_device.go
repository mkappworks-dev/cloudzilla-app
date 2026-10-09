package view

import "github.com/mkappworks-dev/cloudzilla-app/internal/view/components"

type DeviceEntryData struct {
	BasePage
	Error string
}

type DeviceConfirmData struct {
	BasePage
	Username    string
	UserCode    string // XXXX-XXXX
	DeviceName  string // already cleaned; empty means unknown
	RequesterIP string
	RequestedAt string // relative, e.g. "just now"
	Scopes      []string
	Confirm     components.ConfirmFactors
	Error       string
}

type DeviceDoneData struct {
	BasePage
	Approved bool
}
