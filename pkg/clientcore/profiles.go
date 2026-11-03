package clientcore

import (
	"encoding/json"
	"os"
)

type Profile struct {
	UserAgent       string
	SecChUa         string
	SecChUaMobile   string
	SecChUaPlatform string
}

type SavedProfile struct {
	Profile
	DeviceJSON string
	BrowserFp  string
}

const profileFile = "vk_profile.json"

func LoadProfileFromDisk() (*SavedProfile, error) {
	data, err := os.ReadFile(profileFile)
	if err != nil {
		return nil, err
	}
	var sp SavedProfile
	if err := json.Unmarshal(data, &sp); err != nil {
		return nil, err
	}
	return &sp, nil
}

func SaveProfileToDisk(sp SavedProfile) error {
	data, err := json.MarshalIndent(sp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(profileFile, data, 0644)
}
