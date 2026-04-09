import API from '../api.js';
import Components from '../components.js';

const Telemetry = {
    chart: null,
    _tab: 'explorer',

    async render() {
        const urlParams = new URLSearchParams(window.location.hash.split('?')[1]);
        const sensorId = urlParams.get('sensor_id') || '';

        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Telemetry & Discovery</h2>
                    <p class="page-subtitle">Visualize sensor data and discover device field mappings</p>
                </div>
            </div>

            <!-- Tab bar -->
            <div style="display:flex;gap:4px;margin-bottom:20px;border-bottom:1px solid var(--border);padding-bottom:0;">
                <button class="telemetry-tab" data-tab="explorer"  style="padding:8px 18px;background:none;border:none;border-bottom:2px solid var(--primary);color:var(--primary);cursor:pointer;font-weight:600;font-size:13px;">Telemetry Explorer</button>
                <button class="telemetry-tab" data-tab="mqtt-disc" style="padding:8px 18px;background:none;border:none;border-bottom:2px solid transparent;color:var(--text-dim);cursor:pointer;font-size:13px;">MQTT Discovery</button>
                <button class="telemetry-tab" data-tab="snmp-walk" style="padding:8px 18px;background:none;border:none;border-bottom:2px solid transparent;color:var(--text-dim);cursor:pointer;font-size:13px;">SNMP Walk</button>
            </div>

            <!-- Explorer tab -->
            <div id="tab-explorer">
                <div style="display:flex;gap:12px;align-items:center;margin-bottom:18px;">
                    <select id="telemetry-sensor-select" style="width:280px;">
                        <option value="">Select Sensor...</option>
                    </select>
                    <button id="btn-refresh-chart" class="btn btn-secondary btn-sm">Refresh</button>
                </div>

                <div class="grid grid-3 mb-20" style="margin-bottom:18px;">
                    <div class="card">
                        <label>Time Range</label>
                        <select id="time-range">
                            <option value="15m">Last 15 Minutes</option>
                            <option value="1h" selected>Last 1 Hour</option>
                            <option value="6h">Last 6 Hours</option>
                            <option value="24h">Last 24 Hours</option>
                            <option value="7d">Last 7 Days</option>
                        </select>
                    </div>
                    <div class="card">
                        <label>Aggregation</label>
                        <select id="agg-func">
                            <option value="MEAN" selected>Average (Mean)</option>
                            <option value="MAX">Maximum</option>
                            <option value="MIN">Minimum</option>
                            <option value="LAST">Last Value</option>
                        </select>
                    </div>
                    <div class="card">
                        <label>Interval</label>
                        <select id="agg-interval">
                            <option value="10s">10 Seconds</option>
                            <option value="1m" selected>1 Minute</option>
                            <option value="5m">5 Minutes</option>
                            <option value="1h">1 Hour</option>
                        </select>
                    </div>
                </div>

                <div class="card">
                    <div class="flex-between mb-20">
                        <div class="card-title">Live Sensor Data</div>
                    </div>
                    <div class="chart-container">
                        <canvas id="telemetry-chart"></canvas>
                    </div>
                </div>

                <div class="card">
                    <div class="card-title">Raw Data Points</div>
                    <div id="raw-telemetry-table"></div>
                </div>
            </div>

            <!-- MQTT Discovery tab -->
            <div id="tab-mqtt-disc" class="hidden">
                <div class="card">
                    <div class="card-title">MQTT Device Discovery</div>
                    <p class="page-subtitle" style="margin-bottom:16px;">
                        Subscribe to MQTT topics on a broker and capture incoming messages to discover device data structure.
                        The Qube will subscribe for the specified duration and return all unique field mappings found.
                    </p>
                    <div class="grid grid-2" style="gap:14px;margin-bottom:16px;">
                        <div class="form-group">
                            <label>Target Qube</label>
                            <select id="mqtt-disc-qube"></select>
                        </div>
                        <div class="form-group">
                            <label>Broker Host</label>
                            <input type="text" id="mqtt-disc-host" placeholder="192.168.1.10 or broker.example.com">
                        </div>
                        <div class="form-group">
                            <label>Broker Port</label>
                            <input type="number" id="mqtt-disc-port" value="1883">
                        </div>
                        <div class="form-group">
                            <label>Topic Filter <span style="color:var(--text-dim);font-size:11px;">(supports wildcards + and #)</span></label>
                            <input type="text" id="mqtt-disc-topic" placeholder="#" value="#">
                        </div>
                        <div class="form-group">
                            <label>Username <span style="color:var(--text-dim);font-size:11px;">(optional)</span></label>
                            <input type="text" id="mqtt-disc-user" placeholder="">
                        </div>
                        <div class="form-group">
                            <label>Password <span style="color:var(--text-dim);font-size:11px;">(optional)</span></label>
                            <input type="password" id="mqtt-disc-pass" placeholder="">
                        </div>
                    </div>
                    <button id="btn-mqtt-discover" class="btn btn-primary">Start Discovery (30s)</button>
                </div>

                <div id="mqtt-disc-results" class="hidden">
                    <div class="card">
                        <div class="flex-between mb-20">
                            <div class="card-title">Received Messages</div>
                            <span id="mqtt-disc-count" class="badge badge-blue">0 messages</span>
                        </div>
                        <p class="page-subtitle" style="margin-bottom:12px;">
                            Below are the topics and JSON fields received. Use these to define measurements in your device template.
                            Check the <strong>JSON Path</strong> column — copy it to the "JSON Path" field in your template's measurement rows.
                        </p>
                        <div id="mqtt-disc-table"></div>
                    </div>

                    <div class="card">
                        <div class="card-title">Mapping Guide</div>
                        <p class="page-subtitle" style="margin-bottom:12px;">Once you've identified your device's fields, create a device template:</p>
                        <ol style="color:var(--text-dim);font-size:13px;line-height:2;padding-left:20px;">
                            <li>Go to <strong>Device Templates</strong> → Create New Template → select <strong>MQTT</strong></li>
                            <li>In <strong>Measurements</strong>, add one row per field you want to capture</li>
                            <li>Set the <strong>MQTT Topic</strong> from the topic column below</li>
                            <li>Set the <strong>JSON Path</strong> (e.g. <code>$.temperature</code>) from the path column below</li>
                            <li>Set the <strong>Field Key</strong> — the name stored in InfluxDB</li>
                            <li>Create a reader for your broker, then add a sensor using your new template</li>
                        </ol>
                    </div>
                </div>
            </div>

            <!-- SNMP Walk tab -->
            <div id="tab-snmp-walk" class="hidden">
                <div class="card">
                    <div class="card-title">SNMP Device Walk</div>
                    <p class="page-subtitle" style="margin-bottom:16px;">
                        Perform an SNMP walk on a device to discover all available OIDs and their current values.
                        Use the results to define OID mappings in your SNMP device template.
                    </p>
                    <div class="grid grid-2" style="gap:14px;margin-bottom:16px;">
                        <div class="form-group">
                            <label>Target Qube <span style="color:var(--text-dim);font-size:11px;">(walk runs from Qube)</span></label>
                            <select id="snmp-walk-qube"></select>
                        </div>
                        <div class="form-group">
                            <label>Device IP Address</label>
                            <input type="text" id="snmp-walk-host" placeholder="192.168.1.20">
                        </div>
                        <div class="form-group">
                            <label>SNMP Port</label>
                            <input type="number" id="snmp-walk-port" value="161">
                        </div>
                        <div class="form-group">
                            <label>Community String</label>
                            <input type="text" id="snmp-walk-community" value="public">
                        </div>
                        <div class="form-group">
                            <label>SNMP Version</label>
                            <select id="snmp-walk-version">
                                <option value="2c" selected>v2c</option>
                                <option value="1">v1</option>
                                <option value="3">v3</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label>Root OID <span style="color:var(--text-dim);font-size:11px;">(leave blank for full walk)</span></label>
                            <input type="text" id="snmp-walk-oid" placeholder=".1.3.6.1" value=".1.3.6.1">
                        </div>
                    </div>
                    <div style="display:flex;gap:10px;align-items:center;">
                        <button id="btn-snmp-walk" class="btn btn-primary">Run SNMP Walk</button>
                        <span id="snmp-walk-status" style="font-size:12px;color:var(--text-dim);"></span>
                    </div>
                </div>

                <div id="snmp-walk-results" class="hidden">
                    <div class="card">
                        <div class="flex-between mb-20">
                            <div class="card-title">OID Results</div>
                            <div style="display:flex;gap:8px;">
                                <input type="text" id="snmp-walk-filter" placeholder="Filter OIDs..." style="width:200px;font-size:12px;">
                                <span id="snmp-walk-count" class="badge badge-blue">0 OIDs</span>
                            </div>
                        </div>
                        <p class="page-subtitle" style="margin-bottom:12px;">
                            Copy the <strong>OID</strong> value into your SNMP device template's measurement rows.
                            Set a <strong>Field Key</strong> (name for this measurement) and optionally a <strong>Scale</strong> factor.
                        </p>
                        <div id="snmp-walk-table"></div>
                    </div>

                    <div class="card">
                        <div class="card-title">Mapping Guide</div>
                        <p class="page-subtitle" style="margin-bottom:12px;">Once you've found your OIDs, create a device template:</p>
                        <ol style="color:var(--text-dim);font-size:13px;line-height:2;padding-left:20px;">
                            <li>Go to <strong>Device Templates</strong> → Create New Template → select <strong>SNMP</strong></li>
                            <li>In <strong>Measurements</strong>, add one row per OID you want to monitor</li>
                            <li>Set the <strong>OID</strong> from the results below</li>
                            <li>Set the <strong>Field Key</strong> — name stored in InfluxDB (e.g. <code>battery_pct</code>)</li>
                            <li>Set <strong>Scale</strong> if the raw value needs conversion (e.g. <code>0.1</code> for tenths)</li>
                            <li>The Per-Device Parameters (host, community, port) are filled when adding a sensor</li>
                        </ol>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        await this.loadSensors();
        await this.loadQubes();

        const urlParams = new URLSearchParams(window.location.hash.split('?')[1]);
        const sensorId = urlParams.get('sensor_id');
        if (sensorId) {
            const sel = document.getElementById('telemetry-sensor-select');
            if (sel) sel.value = sensorId;
            this.updateChart();
        }

        // Tab switching
        document.querySelectorAll('.telemetry-tab').forEach(btn => {
            btn.addEventListener('click', () => this.switchTab(btn.dataset.tab));
        });

        document.getElementById('telemetry-sensor-select')?.addEventListener('change', () => this.updateChart());
        document.querySelectorAll('#tab-explorer select').forEach(sel => {
            if (sel.id !== 'telemetry-sensor-select') sel.addEventListener('change', () => this.updateChart());
        });
        document.getElementById('btn-refresh-chart')?.addEventListener('click', () => this.updateChart());

        // MQTT discovery
        document.getElementById('btn-mqtt-discover')?.addEventListener('click', () => this.runMqttDiscovery());

        // SNMP walk
        document.getElementById('btn-snmp-walk')?.addEventListener('click', () => this.runSnmpWalk());
        document.getElementById('snmp-walk-filter')?.addEventListener('input', (e) => this.filterSnmpResults(e.target.value));
    },

    switchTab(tab) {
        this._tab = tab;
        document.querySelectorAll('.telemetry-tab').forEach(btn => {
            const active = btn.dataset.tab === tab;
            btn.style.borderBottomColor = active ? 'var(--primary)' : 'transparent';
            btn.style.color = active ? 'var(--primary)' : 'var(--text-dim)';
            btn.style.fontWeight = active ? '600' : '400';
        });
        ['explorer', 'mqtt-disc', 'snmp-walk'].forEach(t => {
            const el = document.getElementById(`tab-${t}`);
            if (el) el.classList.toggle('hidden', t !== tab);
        });
    },

    async loadSensors() {
        const select = document.getElementById('telemetry-sensor-select');
        if (!select) return;
        try {
            const qubes = await API.getQubes();
            for (const q of qubes) {
                const readers = await API.getQubeReaders(q.id);
                for (const r of readers) {
                    const sensors = await API.getReaderSensors(r.id);
                    sensors.forEach(s => {
                        const opt = document.createElement('option');
                        opt.value = s.id;
                        opt.textContent = `${s.name} (${q.id} / ${r.name})`;
                        select.appendChild(opt);
                    });
                }
            }
        } catch (err) {
            console.error('Failed to load sensors for telemetry', err);
        }
    },

    async loadQubes() {
        try {
            const qubes = await API.getQubes();
            ['mqtt-disc-qube', 'snmp-walk-qube'].forEach(id => {
                const sel = document.getElementById(id);
                if (!sel) return;
                qubes.forEach(q => {
                    const opt = document.createElement('option');
                    opt.value = q.id;
                    opt.textContent = `${q.id} — ${q.location_label || q.hostname || ''}`;
                    sel.appendChild(opt);
                });
            });
        } catch (err) {
            console.error('Failed to load qubes', err);
        }
    },

    async updateChart() {
        const sensorId = document.getElementById('telemetry-sensor-select')?.value;
        if (!sensorId) return;

        const range    = document.getElementById('time-range')?.value;
        const aggFunc  = document.getElementById('agg-func')?.value;
        const interval = document.getElementById('agg-interval')?.value;

        try {
            const data = await API.getTelemetry({ sensor_id: sensorId, range, agg_func: aggFunc, interval })
                .catch(() => this.getMockData());
            this.renderChart(data);
            this.renderTable(data);
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    // ── MQTT Discovery ────────────────────────────────────────────────────────

    async runMqttDiscovery() {
        const qubeId    = document.getElementById('mqtt-disc-qube')?.value;
        const host      = document.getElementById('mqtt-disc-host')?.value.trim();
        const port      = parseInt(document.getElementById('mqtt-disc-port')?.value) || 1883;
        const topic     = document.getElementById('mqtt-disc-topic')?.value.trim() || '#';
        const username  = document.getElementById('mqtt-disc-user')?.value.trim();
        const password  = document.getElementById('mqtt-disc-pass')?.value;

        if (!qubeId || !host) {
            Components.showAlert('Select a Qube and enter a broker host', 'error');
            return;
        }

        const btn = document.getElementById('btn-mqtt-discover');
        btn.disabled = true;
        btn.textContent = 'Discovering... (30s)';

        try {
            // Send mqtt_discover command to the Qube
            const res = await API.post(`/api/v1/qubes/${qubeId}/commands`, {
                command: 'mqtt_discover',
                payload: { broker_host: host, broker_port: port, topic, username, password, duration_sec: 30 }
            });

            // Show immediate feedback; results come via command ack
            Components.showAlert(`Discovery command sent to ${qubeId}. Check command history for results in ~35s.`);
            document.getElementById('mqtt-disc-results').classList.remove('hidden');

            // If the result is already available (synchronous or fast response)
            if (res.result?.messages) {
                this.renderMqttDiscResults(res.result.messages);
            } else {
                document.getElementById('mqtt-disc-table').innerHTML =
                    `<div class="page-subtitle" style="padding:20px 0;">
                        Command sent. Results will appear when the Qube acknowledges (~35 seconds).
                        You can check <strong>Remote Commands → Recent History</strong> for the ack payload.
                    </div>`;
            }
        } catch (err) {
            Components.showAlert(err.message, 'error');
        } finally {
            btn.disabled = false;
            btn.textContent = 'Start Discovery (30s)';
        }
    },

    renderMqttDiscResults(messages) {
        const countEl = document.getElementById('mqtt-disc-count');
        if (countEl) countEl.textContent = `${messages.length} messages`;

        // Flatten to unique topic+field mappings
        const rows = [];
        messages.forEach(msg => {
            const topic = msg.topic;
            const payload = msg.payload;
            try {
                const obj = JSON.parse(payload);
                this._flattenJson(obj, '', topic, rows);
            } catch {
                rows.push({ topic, field: '(raw)', jsonPath: '.', value: payload.substring(0, 80) });
            }
        });

        const container = document.getElementById('mqtt-disc-table');
        if (!rows.length) {
            container.innerHTML = '<div class="page-subtitle" style="padding:10px 0;">No messages received in 30s window.</div>';
            return;
        }

        Components.renderTable(
            ['Topic', 'Field', 'JSON Path', 'Sample Value'],
            rows,
            'mqtt-disc-table',
            r => [
                `<code style="font-size:11px;">${r.topic}</code>`,
                `<code style="font-size:11px;">${r.field}</code>`,
                `<code style="color:var(--primary);font-size:11px;">${r.jsonPath}</code>`,
                `<span style="color:var(--text-dim);font-size:11px;">${r.value}</span>`,
            ]
        );
    },

    _flattenJson(obj, prefix, topic, rows) {
        Object.entries(obj).forEach(([k, v]) => {
            const path = prefix ? `${prefix}.${k}` : k;
            const jsonPath = `$.${path}`;
            if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
                this._flattenJson(v, path, topic, rows);
            } else {
                const existing = rows.find(r => r.topic === topic && r.jsonPath === jsonPath);
                if (!existing) {
                    rows.push({ topic, field: k, jsonPath, value: String(v).substring(0, 80) });
                }
            }
        });
    },

    // ── SNMP Walk ─────────────────────────────────────────────────────────────

    _snmpRows: [],

    async runSnmpWalk() {
        const qubeId    = document.getElementById('snmp-walk-qube')?.value;
        const host      = document.getElementById('snmp-walk-host')?.value.trim();
        const port      = parseInt(document.getElementById('snmp-walk-port')?.value) || 161;
        const community = document.getElementById('snmp-walk-community')?.value.trim() || 'public';
        const version   = document.getElementById('snmp-walk-version')?.value || '2c';
        const rootOid   = document.getElementById('snmp-walk-oid')?.value.trim() || '.1.3.6.1';

        if (!qubeId || !host) {
            Components.showAlert('Select a Qube and enter a device IP', 'error');
            return;
        }

        const btn = document.getElementById('btn-snmp-walk');
        const status = document.getElementById('snmp-walk-status');
        btn.disabled = true;
        btn.textContent = 'Walking...';
        if (status) status.textContent = 'Sending walk command to Qube...';

        try {
            const res = await API.post(`/api/v1/qubes/${qubeId}/commands`, {
                command: 'snmp_walk',
                payload: { host, port, community, version, root_oid: rootOid }
            });

            document.getElementById('snmp-walk-results').classList.remove('hidden');

            if (res.result?.oids) {
                this._snmpRows = res.result.oids;
                this.renderSnmpWalkResults(this._snmpRows);
                if (status) status.textContent = `Found ${this._snmpRows.length} OIDs`;
            } else {
                if (status) status.textContent = 'Command sent — results appear in ~10s via command ack.';
                document.getElementById('snmp-walk-table').innerHTML =
                    `<div class="page-subtitle" style="padding:20px 0;">
                        Command sent to ${qubeId}. Results will appear when the Qube acknowledges.
                        Check <strong>Remote Commands → Recent History</strong> for the ack payload with OID data.
                    </div>`;
            }
        } catch (err) {
            Components.showAlert(err.message, 'error');
            if (status) status.textContent = '';
        } finally {
            btn.disabled = false;
            btn.textContent = 'Run SNMP Walk';
        }
    },

    renderSnmpWalkResults(oids) {
        const count = document.getElementById('snmp-walk-count');
        if (count) count.textContent = `${oids.length} OIDs`;

        Components.renderTable(
            ['OID', 'Type', 'Value'],
            oids,
            'snmp-walk-table',
            r => [
                `<code style="font-size:11px;color:var(--primary);">${r.oid}</code>`,
                `<span style="font-size:11px;color:var(--text-dim);">${r.type || 'unknown'}</span>`,
                `<span style="font-size:12px;">${String(r.value || '').substring(0, 120)}</span>`,
            ]
        );
    },

    filterSnmpResults(query) {
        if (!this._snmpRows.length) return;
        const filtered = query
            ? this._snmpRows.filter(r => r.oid.includes(query) || String(r.value || '').toLowerCase().includes(query.toLowerCase()))
            : this._snmpRows;
        this.renderSnmpWalkResults(filtered);
    },

    // ── Chart / Table ─────────────────────────────────────────────────────────

    renderChart(data) {
        const ctx = document.getElementById('telemetry-chart')?.getContext('2d');
        if (!ctx) return;

        if (this.chart) this.chart.destroy();

        const datasets = [];
        const fields = Object.keys(data[0] || {}).filter(k => k !== 'time');
        const colors = ['#7c85ff', '#63b3ed', '#b794f4', '#68d391', '#fc8181', '#ecc94b'];

        fields.forEach((field, i) => {
            datasets.push({
                label: field,
                data: data.map(d => ({ x: new Date(d.time), y: d[field] })),
                borderColor: colors[i % colors.length],
                backgroundColor: colors[i % colors.length] + '22',
                borderWidth: 2,
                tension: 0.3,
                fill: true,
                pointRadius: 0
            });
        });

        this.chart = new Chart(ctx, {
            type: 'line',
            data: { datasets },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { intersect: false, mode: 'index' },
                scales: {
                    x: { type: 'time', time: { unit: 'minute' }, grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#718096' } },
                    y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#718096' } }
                },
                plugins: { legend: { labels: { color: '#e2e8f0', usePointStyle: true } } }
            }
        });
    },

    renderTable(data) {
        if (!data || data.length === 0) return;
        const fields = Object.keys(data[0]);
        Components.renderTable(
            fields,
            data.slice(-10).reverse(),
            'raw-telemetry-table',
            (d) => fields.map(f => f === 'time' ? new Date(d[f]).toLocaleTimeString() : d[f])
        );
    },

    getMockData() {
        const data = [];
        const now = Date.now();
        for (let i = 0; i < 60; i++) {
            data.push({
                time: new Date(now - (60 - i) * 60000).toISOString(),
                value: 50 + Math.random() * 20,
                status: Math.random() > 0.1 ? 1 : 0
            });
        }
        return data;
    }
};

export default Telemetry;
