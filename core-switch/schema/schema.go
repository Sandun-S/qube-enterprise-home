package schema

// DataIn is the universal data format for all readers → core-switch.
// All readers MUST use this struct when sending data.
//
// Field rules:
//   - Output: "influxdb", "live", or "influxdb,live" (comma-separated, no spaces)
//   - Time:   Unix MICROSECONDS (time.Now().UnixMicro())
//   - Value:  Always a string, even for numeric values
//   - Tags:   Comma-separated key=value pairs: "name=PM5100,location=rack1"
type DataIn struct {
	Table     string `json:"Table"`
	Equipment string `json:"Equipment"`
	Reading   string `json:"Reading"`
	Output    string `json:"Output"`
	Sender    string `json:"Sender"`
	Tags      string `json:"Tags"`      // field1=value,f2=v2
	Time      int64  `json:"Time"`
	Value     string `json:"Value"`
}

// Alert is the universal alert format.
type Alert struct {
	Sender  string `json:"Sender"`
	Message string `json:"Message"`
	Type    string `json:"Type"` // connectivity, data, other
	Mode    int    `json:"Mode"` // 0=resolved, 1=complain
}
