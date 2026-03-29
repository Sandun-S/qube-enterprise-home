package modbus

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	modbusclient "github.com/simonvetter/modbus"
	"github.com/sirupsen/logrus"

	"github.com/qube-enterprise/pkg/coreswitch"
)

// ModbusConfig holds Modbus connection settings.
type ModbusConfig struct {
	Server          string // "modbus-tcp://<host>:<port>"
	FreqSec         int
	SingleReadCount int
	SlaveID         int
}

// Reading represents a single register reading definition.
type Reading struct {
	Equipment string
	Reading   string
	RegType   string   // "Holding" or "Input"
	Addr      uint16
	DataType  string   // uint16, int16, uint32, int32, float32
	Output    string   // "influxdb", "live", "influxdb,live"
	Table     string   // InfluxDB table name
	Tags      string   // "key=val,key2=val2"
	Scale     float64
}

// readGroup groups readings by equipment+regType for batch sending.
type readGroup struct {
	key      string
	readings []*Reading
}

var log *logrus.Logger
var conf *ModbusConfig
var cs *coreswitch.Client
var conError bool

var mapRegTypeGrp  map[string][]*readGroup
var mapRegTypeAddr map[string][]uint16

// Init sets up the modbus polling engine.
// Sorts readings, groups by equipment+regType, deduplicates addresses, starts timer.
func Init(l *logrus.Logger, recs []*Reading, c *ModbusConfig, client *coreswitch.Client) {

	log = l
	conError = false
	conf = c
	cs = client

	if conf.FreqSec == 0 {
		conf.FreqSec = 5
	}
	if conf.SingleReadCount == 0 {
		conf.SingleReadCount = 100
	}

	// Sort readings by regType then address (production pattern)
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].RegType == recs[j].RegType {
			return recs[i].Addr < recs[j].Addr
		}
		return recs[i].RegType[0] < recs[j].RegType[0]
	})

	// Build grouping maps
	mapKeyGrp := make(map[string]*readGroup)
	mapRegTypeGrp = make(map[string][]*readGroup)
	mapRegTypeAddr = make(map[string][]uint16)

	for _, rec := range recs {
		if _, ok := mapRegTypeGrp[rec.RegType]; !ok {
			mapRegTypeGrp[rec.RegType] = make([]*readGroup, 0)
			mapRegTypeAddr[rec.RegType] = make([]uint16, 0)
		}

		key := fmt.Sprintf("%s:%s", rec.Equipment, rec.RegType)
		group, ok := mapKeyGrp[key]
		if !ok {
			group = &readGroup{key: key, readings: make([]*Reading, 0)}
			mapKeyGrp[key] = group
			mapRegTypeGrp[rec.RegType] = append(mapRegTypeGrp[rec.RegType], group)
		}

		mapRegTypeAddr[rec.RegType] = append(mapRegTypeAddr[rec.RegType], rec.Addr)
		group.readings = append(group.readings, rec)
	}

	// Dedup addresses per regType (sorted input — adjacent dedup)
	for rtype := range mapRegTypeAddr {
		addrs := mapRegTypeAddr[rtype]
		if len(addrs) == 0 {
			continue
		}
		i := 0
		for j := 1; j < len(addrs); j++ {
			if addrs[i] != addrs[j] {
				i++
				addrs[i] = addrs[j]
			}
		}
		mapRegTypeAddr[rtype] = addrs[:i+1]
	}

	log.Infof("%d reading(s) on %d equipment(s) loaded", len(recs), len(mapKeyGrp))

	time.AfterFunc(time.Duration(conf.FreqSec)*time.Second, onTimer)

	log.Info("Modbus reader started")
}

func onTimer() {
	defer time.AfterFunc(time.Duration(conf.FreqSec)*time.Second, onTimer)

	log.Info("Timer fired for data reading")
	fetchData()
}

// fetchData connects to the Modbus server, reads registers in blocks, and
// sends grouped data to core-switch. Mirrors production PLC4x block-read logic.
func fetchData() {

	// Parse host:port from "modbus-tcp://<host>:<port>"
	serverAddr := conf.Server
	if len(serverAddr) > 13 && serverAddr[:13] == "modbus-tcp://" {
		serverAddr = serverAddr[13:]
	}

	client, err := modbusclient.NewClient(&modbusclient.ClientConfiguration{
		URL:     "tcp://" + serverAddr,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		log.Errorf("Cannot create Modbus client: %v", err)
		sendConnAlert(fmt.Sprintf("modbus-reader cannot create client for %s - %v", conf.Server, err))
		return
	}

	err = client.Open()
	if err != nil {
		log.Errorf("Cannot connect to Modbus server %s: %v", conf.Server, err)
		sendConnAlert(fmt.Sprintf("modbus-reader cannot connect to %s - %v", conf.Server, err))
		return
	}
	defer client.Close()

	client.SetUnitId(uint8(conf.SlaveID))

	// Connectivity restored
	if conError {
		cs.SendAlert("Connectivity",
			fmt.Sprintf("modbus-reader - %s connectivity restored", conf.Server), 0)
		conError = false
	}

	log.Infof("Connected to Modbus server: %s", conf.Server)

	// Loop through register types (Holding, Input)
	for rtype, gLst := range mapRegTypeGrp {

		addrs := mapRegTypeAddr[rtype]
		ln := len(addrs)
		if ln == 0 {
			log.Infof("Nothing to read on %s registers", rtype)
			continue
		}

		regs := make(map[uint16][]uint16) // addr → raw uint16 values

		startIndx := 0

		// Block read loop (read SingleReadCount addresses at a time)
		for startIndx < ln {
			count := uint16(conf.SingleReadCount)
			startAddr := addrs[startIndx]

			// Find last index within this block
			lastIndx := startIndx
			for idx := startIndx; idx < ln; idx++ {
				if addrs[idx] >= startAddr+count {
					break
				}
				lastIndx = idx
			}

			count = addrs[lastIndx] - startAddr + 1
			count = adjustCountForDataTypes(startAddr, count, gLst)

			log.Infof("Requesting %s... start:%d end:%d count:%d", rtype, startAddr, startAddr+count-1, count)

			var rawRegs []uint16

			if rtype == "Holding" {
				rawRegs, err = client.ReadRegisters(startAddr, count, modbusclient.HOLDING_REGISTER)
			} else if rtype == "Input" {
				rawRegs, err = client.ReadRegisters(startAddr, count, modbusclient.INPUT_REGISTER)
			} else {
				log.Warnf("Unknown register type: %s", rtype)
				startIndx = lastIndx + 1
				continue
			}

			if err != nil {
				log.Errorf("Error reading %s registers at %d: %v", rtype, startAddr, err)
				sendConnAlert(fmt.Sprintf("modbus-reader read error on %s at addr %d - %v", conf.Server, startAddr, err))
				return
			}

			if len(rawRegs) != int(count) {
				log.Errorf("Expected %d registers, got %d", count, len(rawRegs))
				return
			}

			for idx := uint16(0); idx < count; idx++ {
				regs[startAddr+idx] = []uint16{rawRegs[idx]}
			}

			startIndx = lastIndx + 1
		}

		// Send grouped data to core-switch
		timeval := time.Now().UnixMicro()

		for _, grp := range gLst {
			arr := make([]coreswitch.DataIn, 0, len(grp.readings))

			for _, rec := range grp.readings {
				val := decodeValue(rec, regs)

				d := coreswitch.DataIn{
					Table:     rec.Table,
					Equipment: rec.Equipment,
					Reading:   rec.Reading,
					Output:    rec.Output,
					Sender:    "modbus-reader",
					Tags:      rec.Tags,
					Time:      timeval,
					Value:     val,
				}

				log.Debugf("Reading %s.%s addr:%d = %s", rec.Equipment, rec.Reading, rec.Addr, val)
				arr = append(arr, d)
			}

			if err := cs.SendBatch(arr); err != nil {
				log.Errorf("Error sending data to core-switch: %v", err)
			} else {
				log.Debugf("%d readings sent to core-switch", len(arr))
			}
		}
	}

	log.Info("Read cycle complete")
}

// adjustCountForDataTypes ensures the block read includes enough registers
// for 32-bit data types that span 2 registers.
func adjustCountForDataTypes(startAddr, count uint16, groups []*readGroup) uint16 {
	maxAddr := startAddr + count - 1
	for _, grp := range groups {
		for _, rec := range grp.readings {
			if rec.Addr >= startAddr && rec.Addr <= maxAddr {
				switch rec.DataType {
				case "uint32", "int32", "float32":
					needed := rec.Addr + 2
					if needed-startAddr > count {
						count = needed - startAddr
					}
				}
			}
		}
	}
	return count
}

// decodeValue reads raw uint16 register values and converts to the specified data type,
// applying the scale factor.
func decodeValue(rec *Reading, regs map[uint16][]uint16) string {
	raw, ok := regs[rec.Addr]
	if !ok || len(raw) == 0 {
		return "0"
	}

	var floatVal float64

	switch rec.DataType {
	case "uint16":
		floatVal = float64(raw[0])

	case "int16":
		floatVal = float64(int16(raw[0]))

	case "uint32":
		hi := raw[0]
		lo := uint16(0)
		if r, ok := regs[rec.Addr+1]; ok && len(r) > 0 {
			lo = r[0]
		}
		floatVal = float64(uint32(hi)<<16 | uint32(lo))

	case "int32":
		hi := raw[0]
		lo := uint16(0)
		if r, ok := regs[rec.Addr+1]; ok && len(r) > 0 {
			lo = r[0]
		}
		floatVal = float64(int32(uint32(hi)<<16 | uint32(lo)))

	case "float32":
		hi := raw[0]
		lo := uint16(0)
		if r, ok := regs[rec.Addr+1]; ok && len(r) > 0 {
			lo = r[0]
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint16(buf[0:2], hi)
		binary.BigEndian.PutUint16(buf[2:4], lo)
		floatVal = float64(math.Float32frombits(binary.BigEndian.Uint32(buf)))

	default:
		floatVal = float64(raw[0])
	}

	floatVal *= rec.Scale

	if floatVal == math.Trunc(floatVal) && math.Abs(floatVal) < 1e15 {
		return fmt.Sprintf("%.0f", floatVal)
	}
	return fmt.Sprintf("%.6f", floatVal)
}

func sendConnAlert(msg string) {
	conError = true
	cs.SendAlert("Connectivity", msg, 1)
}
