package datasources

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/shirou/gopsutil/v3/host"
	log "github.com/sirupsen/logrus"

	"github.com/dkaser/unraid-motd/utils"
)

// ConfTempCPU extends ConfBase with a list of containers to ignore
type ConfTempCPU struct {
	ConfBaseWarn `yaml:",inline"`
}

// Init sets up default alignment
func (c *ConfTempCPU) Init() {
	c.ConfBaseWarn.Init()
}

// GetCPUTemp returns CPU core temps using gopsutil or parsing sensors output
func GetCPUTemp(ch chan<- SourceReturn, conf *Conf) {
	c := conf.CPU
	c.Load(conf)

	sr := NewSourceReturn(conf.debug)
	defer func() {
		ch <- sr.Return(&c.ConfBase)
	}()
	var tempMap map[string]int
	var isZen bool
	var err error
	tempMap, isZen, err = cpuTempGopsutil()

	if err != nil {
		log.Warnf("[cpu] temperature read error: %v", err)
	}

	if len(tempMap) == 0 {
		err = &ModuleNotAvailable{"cpu", err}

		t := GetTableWriter(c)
		sr.Content = RenderTable(t, "CPU Temp: " + utils.Warn("Unavailable"))
	} else {
		sr.Content, sr.Error = formatCPUTemps(tempMap, isZen, &c)
	}
}

func formatCPUTemps(tempMap map[string]int, isZen bool, c *ConfTempCPU) (content string, err error) {
	t := GetTableWriter(c)
	var title string

	// Sort keys
	sortedNames := make([]string, len(tempMap))
	i := 0
	for k := range tempMap {
		sortedNames[i] = k
		i++
	}
	sort.Strings(sortedNames)
	var warnCount int
	var errCount int
	for _, k := range sortedNames {
		v := tempMap[k]
		var wrapped string
		if !isZen {
			wrapped = fmt.Sprintf("Core %s", k)
		} else {
			wrapped = k
		}
		if v < c.Warn && !*c.WarnOnly {
			t.AppendRow([]interface{}{wrapped, utils.Good(v)})
		} else if v >= c.Warn && v < c.Crit {
			t.AppendRow([]interface{}{wrapped, utils.Warn(v)})
			warnCount++
		} else if v >= c.Crit {
			warnCount++
			errCount++
			t.AppendRow([]interface{}{wrapped, utils.Err(v)})
		}
	}
	if warnCount == 0 {
		title = fmt.Sprintf("%s: %s", "CPU Temp", utils.Good("OK"))
	} else if errCount > 0 {
		title = fmt.Sprintf("%s: %s", "CPU Temp", utils.Err("Critical"))
	} else if warnCount > 0 {
		title = fmt.Sprintf("%s: %s", "CPU Temp", utils.Warn("Warning"))
	}

	content = RenderTable(t, title)
	return
}

func cpuTempGopsutil() (tempMap map[string]int, isZen bool, err error) {
	temps, err := host.SensorsTemperatures()
	tempMap = make(map[string]int)
	addTemp := func(re *regexp.Regexp) {
		for _, stat := range temps {
			log.Debugf("[cpu] check %s", stat.SensorKey)
			m := re.FindStringSubmatch(stat.SensorKey)
			if len(m) > 1 {
				log.Debugf("[cpu] OK %s: %.0f", stat.SensorKey, stat.Temperature)
				tempMap[m[1]] = int(stat.Temperature)
			}
		}
	}
	addTemp(regexp.MustCompile(`coretemp_core(?:_)?(\d+)`))
	// Try k10temp if we didn't find anything
	if len(tempMap) == 0 {
		isZen = true
		log.Debug("[cpu] trying k10temp")
		addTemp(regexp.MustCompile(`k10temp_(\w+)`))
	}
	// Something's really wrong if we still have none
	if len(tempMap) == 0 {
		log.Warn("[cpu] could not find any CPU temperatures")
	} else {
		err = nil
	}
	return
}
