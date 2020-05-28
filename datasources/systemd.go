package datasources

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/cosandr/go-motd/utils"
)

// SystemdConf extends CommonConf with a list of units to monitor
type SystemdConf struct {
	CommonConf `yaml:",inline"`
	Units      []string `yaml:"units"`
	HideExt    bool     `yaml:"hideExt"`
	InactiveOK bool     `yaml:"inactiveOK"`
	ShowFailed bool     `yaml:"showFailed"`
}

type systemdUnit struct {
	Name           string
	ActiveState    string
	Result         string
	ExecMainStatus string
	LoadState      string
}

// IsEmpty returns true if we have no information about unit state
func (s *systemdUnit) IsEmpty() bool {
	return s.ActiveState == "" && s.Result == "" && s.ExecMainStatus == "" && s.LoadState == ""
}

// GetProperties gets this unit's properties from DBus
func (s *systemdUnit) GetProperties(con *dbus.Conn) (err error) {
	// Do nothing if we already have everything
	if s.ActiveState != "" && s.Result != "" && s.ExecMainStatus != "" && s.LoadState != "" {
		return
	}
	props, err := con.GetUnitProperties(s.Name)
	if err != nil {
		return
	}
	if s.ActiveState == "" {
		if data, ok := props["ActiveState"].(string); ok {
			s.ActiveState = data
		}
	}
	if s.Result == "" {
		if data, ok := props["Result"].(string); ok {
			s.Result = data
		}
	}
	if s.ExecMainStatus == "" {
		if data, ok := props["ExecMainStatus"].(int32); ok {
			s.ExecMainStatus = strconv.Itoa(int(data))
		}
	}
	if s.LoadState == "" {
		if data, ok := props["LoadState"].(string); ok {
			s.LoadState = data
		}
	}
	return
}

// GetSystemd gets systemd unit status using dbus
func GetSystemd(ret chan<- string, c *SystemdConf) {
	header, content, _ := getServiceStatus(c.Units, *c.FailedOnly, c.HideExt, c.InactiveOK, c.ShowFailed)
	// Pad header
	var p = utils.Pad{Delims: map[string]int{padL: c.Header[0], padR: c.Header[1]}, Content: header}
	header = p.Do()
	if len(content) == 0 {
		ret <- header
		return
	}
	// Pad container list
	p = utils.Pad{Delims: map[string]int{padL: c.Content[0], padR: c.Content[1]}, Content: content}
	content = p.Do()
	ret <- header + "\n" + content
}

// getServiceStatus get service properties
func getServiceStatus(reqUnits []string, failedOnly bool, hideExt bool, inactiveOK bool, showFailed bool) (header string, content string, err error) {
	con, err := dbus.New()
	if err != nil {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("Systemd", padL, padR), utils.Err("DBus failed"))
		return
	}
	defer con.Close()
	// No units to check and didn't request to show failed
	if len(reqUnits) == 0 && !showFailed {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("Systemd", padL, padR), utils.Warn("unconfigured"))
		return
	}
	units := make([]systemdUnit, 0)
	if showFailed {
		// Get all failed
		listFailed, _ := con.ListUnitsFiltered([]string{"failed"})
		if len(listFailed) > 0 {
			for _, u := range listFailed {
				units = append(units, systemdUnit{
					Name:        u.Name,
					ActiveState: u.ActiveState,
					LoadState:   u.LoadState,
				})
			}
		}
	}
	if len(reqUnits) > 0 {
		for _, name := range reqUnits {
			units = append(units, systemdUnit{
				Name: name,
			})
		}
	}
	var errStr = ""
	// Get missing properties
	for i := range units {
		err = units[i].GetProperties(con)
		if err != nil {
			errStr += fmt.Sprintf("Failed to get properties for %s: %s\n", units[i].Name, err)
			err = nil
		}
	}
	// Map of maps to hold properties
	sort.Slice(units, func(i, j int) bool {
		return units[i].Name < units[j].Name
	})
	// Maps to make checking easier later
	var failedUnits = map[string]string{}
	var goodUnits = map[string]string{}
	// Loop through units so it is alphabetical
	for _, u := range units {
		// Skip if we have no stats
		if u.IsEmpty() {
			continue
		}
		wrapped := utils.Wrap(u.Name, padL, padR)
		if hideExt {
			// Remove all systemd extensions
			re := regexp.MustCompile(`(\.service|\.socket|\.device|\.mount|\.automount|\.swap|\.target|\.path|\.timer|\.slice|\.scope)`)
			wrapped = re.ReplaceAllString(wrapped, "")
		}
		// No such unit file
		if u.LoadState != "loaded" {
			failedUnits[u.Name] = fmt.Sprintf("%s: %s\n", wrapped, utils.Err(u.LoadState))
		} else {
			// Service running
			if u.ActiveState == "active" {
				goodUnits[u.Name] = fmt.Sprintf("%s: %s\n", wrapped, utils.Good(u.ActiveState))
			} else {
				// Not running but existed successfully
				if u.ExecMainStatus == "0" {
					if inactiveOK {
						goodUnits[u.Name] = fmt.Sprintf("%s: %s\n", wrapped, utils.Good(u.Result))
					} else {
						failedUnits[u.Name] = fmt.Sprintf("%s: %s\n", wrapped, utils.Warn(u.ActiveState))
					}
					// Not running and failed
				} else {
					failedUnits[u.Name] = fmt.Sprintf("%s: %s\n", wrapped, utils.Err(u.ActiveState))
				}
			}
		}
	}
	// Decide what header should be
	// Only print all services if requested
	if len(goodUnits) == 0 {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("Systemd", padL, padR), utils.Err("critical"))
	} else if len(failedUnits) == 0 {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("Systemd", padL, padR), utils.Good("OK"))
		if failedOnly {
			return
		}
	} else if len(failedUnits) < len(units) {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("Systemd", padL, padR), utils.Warn("warning"))
	}
	// Print all in order
	for _, u := range units {
		if val, ok := goodUnits[u.Name]; ok && !failedOnly {
			content += val
		} else if val, ok := failedUnits[u.Name]; ok {
			content += val
		}
	}
	if len(errStr) > 0 {
		content += errStr
	}
	return
}
