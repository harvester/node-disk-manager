package utils

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"time"
)

type Disk struct {
	XMLName xml.Name `xml:"disk"`
	Type    string   `xml:"type,attr"`
	Device  string   `xml:"device,attr"`
	Driver  Driver   `xml:"driver"`
	Source  Source   `xml:"source"`
	Target  Target   `xml:"target"`
	WWN     string   `xml:"wwn"`
	VENDOR  string   `xml:"vendor"`
}

type Driver struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type Source struct {
	File string `xml:"file,attr"`
}

type Target struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

// DiskXMLReader can read the libvirt disk xml file and return a Disk struct
func DiskXMLReader(filePath string) (Disk, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return Disk{}, fmt.Errorf("open file(%s) error: %v", filePath, err)
	}

	defer f.Close()

	var disk Disk
	err = xml.NewDecoder(f).Decode(&disk)
	if err != nil {
		return Disk{}, fmt.Errorf("decode XML Error: %v", err)
	}

	return disk, nil
}

// XMLWriter write XML to target file, make sure your xmlData should valid
func XMLWriter(targetFilePath string, xmlData any) error {
	// Create a new file for writing
	targetFile, err := os.Create(targetFilePath)
	if err != nil {
		return fmt.Errorf("create file(%s) error: %v", targetFilePath, err)
	}
	defer targetFile.Close()

	// Encode the disk data and write it to the output file
	encoder := xml.NewEncoder(targetFile)
	err = encoder.Encode(xmlData)
	if err != nil {
		return fmt.Errorf("encod XML Error: %v", err)
	}

	return nil
}

func GenHash() string {
	timestamp := time.Now().Unix()
	timestampStr := fmt.Sprintf("%d", timestamp)
	hash := sha512.Sum512([]byte(timestampStr))
	hashStr := hex.EncodeToString(hash[:])
	return hashStr[:6]
}
