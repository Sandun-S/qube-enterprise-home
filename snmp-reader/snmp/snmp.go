// Package snmp manages SNMP connections and polling for the snmp-reader.
// Analogous to v1 snmp-gateway/snmp/snmp.go — same SNMPManager pattern,
// same Trigger() entry point. In v2 devices come from SQLite instead of CSV.
package snmp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/Sandun-S/qube-enterprise-home/snmp-reader/configs"
	"github.com/Sandun-S/qube-enterprise-home/snmp-reader/coreswitch"
)

// Logger is the logging interface expected by SNMPManager.
// *logrus.Logger satisfies this interface.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Debugf(format string, args ...any)
}

// SNMPManager manages SNMP polling across all configured devices.
// Analogous to v1 SNMPManager struct.
type SNMPManager struct {
	devices    []configs.Device
	csClient   *coreswitch.Client
	readerName string
	timeoutMs  int
	retries    int
	log        Logger
}

// Init creates a new SNMPManager ready to poll.
// Analogous to v1 snmp.Init().
func Init(devices []configs.Device, cs *coreswitch.Client, readerName string, timeoutMs, retries int, log Logger) *SNMPManager {
	return &SNMPManager{
		devices:    devices,
		csClient:   cs,
		readerName: readerName,
		timeoutMs:  timeoutMs,
		retries:    retries,
		log:        log,
	}
}

// Trigger polls all SNMP devices once and sends readings to core-switch.
// Called on startup and then on each ticker tick from main.go.
// Analogous to v1 snmp.Trigger().
func (m *SNMPManager) Trigger() {
	for _, dev := range m.devices {
		m.poll(dev)
	}
}

// poll polls a single SNMP device and forwards readings to core-switch.
func (m *SNMPManager) poll(dev configs.Device) {
	client := &gosnmp.GoSNMP{
		Target:    dev.Host,
		Port:      uint16(dev.Port),
		Community: dev.Community,
		Version:   parseVersion(dev.Version),
		Timeout:   time.Duration(m.timeoutMs) * time.Millisecond,
		Retries:   m.retries,
	}

	if err := client.Connect(); err != nil {
		m.log.Warnf("SNMP connect failed for %s (%s): %v", dev.Name, dev.Host, err)
		return
	}
	defer client.Conn.Close()

	oidStrs := make([]string, len(dev.OIDs))
	for i, o := range dev.OIDs {
		oidStrs[i] = o.OID
	}

	result, err := client.Get(oidStrs)
	if err != nil {
		m.log.Warnf("SNMP GET failed for %s (%s): %v", dev.Name, dev.Host, err)
		return
	}

	// Build OID → mapping lookup for response processing
	oidMap := map[string]configs.OIDMapping{}
	for _, o := range dev.OIDs {
		oidMap[o.OID] = o
	}

	var readings []coreswitch.DataIn
	for _, pdu := range result.Variables {
		oid := strings.TrimPrefix(pdu.Name, ".")
		mapping, ok := oidMap[oid]
		if !ok {
			// Some agents return OIDs with a leading dot
			mapping, ok = oidMap["."+oid]
			if !ok {
				continue
			}
		}

		val := extractValue(pdu)
		if val == "" {
			continue
		}

		if mapping.Scale != 1.0 {
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				val = formatFloat(f * mapping.Scale)
			}
		}

		readings = append(readings, coreswitch.DataIn{
			Table:     dev.Table,
			Equipment: dev.Name,
			Reading:   mapping.FieldKey,
			Output:    dev.Output,
			Sender:    m.readerName,
			Tags:      dev.Tags,
			Time:      time.Now().UnixMicro(),
			Value:     val,
		})
	}

	if len(readings) > 0 {
		if err := m.csClient.SendBatch(readings); err != nil {
			m.log.Warnf("Failed to send batch for %s: %v", dev.Name, err)
		} else {
			m.log.Debugf("Sent %d readings for %s (%s)", len(readings), dev.Name, dev.Host)
		}
	}
}

// extractValue converts an SNMP PDU value to a string for core-switch.
func extractValue(pdu gosnmp.SnmpPDU) string {
	switch pdu.Type {
	case gosnmp.Integer:
		return strconv.Itoa(pdu.Value.(int))
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks:
		return strconv.FormatUint(uint64(pdu.Value.(uint)), 10)
	case gosnmp.Counter64:
		return strconv.FormatUint(pdu.Value.(uint64), 10)
	case gosnmp.OctetString:
		return string(pdu.Value.([]byte))
	case gosnmp.ObjectIdentifier:
		return pdu.Value.(string)
	default:
		return fmt.Sprintf("%v", pdu.Value)
	}
}

func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func parseVersion(v string) gosnmp.SnmpVersion {
	switch v {
	case "1":
		return gosnmp.Version1
	case "3":
		return gosnmp.Version3
	default:
		return gosnmp.Version2c
	}
}
