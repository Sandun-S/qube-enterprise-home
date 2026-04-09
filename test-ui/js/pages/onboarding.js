import API from '../api.js';
import Components from '../components.js';

// Protocol → array key for measurements display
const PROTO_ARRAY_KEY = {
    modbus_tcp: 'registers',
    snmp:       'oids',
    mqtt:       'json_paths',
    opcua:      'nodes',
    http:       'json_paths',
};

const Onboarding = {
    state: {
        step: 1,
        qubeId: '',
        protocol: null,
        deviceTemplate: null,
        readerTemplate: null,
        existingReaders: [],
        selectedReaderId: null,  // null = auto-create via smart endpoint
        isMultiTarget: false,
        advancedMode: false,
        readerConnValues: {},    // for endpoint protocols: connection form values
        customSensorConfig: null,
    },

    async render() {
        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">Device Onboarding Wizard</h1>
                    <p class="page-subtitle">Configure a new physical device in simple steps</p>
                </div>
            </div>

            <!-- Progress Bar -->
            <div class="flex-between mb-20" style="margin-bottom:30px;background:var(--bg-sidebar);padding:20px;border-radius:12px;border:1px solid var(--border);">
                <div class="step-indicator active" id="step-1-label">1. Protocol</div>
                <div style="color:var(--border)">→</div>
                <div class="step-indicator" id="step-2-label">2. Template</div>
                <div style="color:var(--border)">→</div>
                <div class="step-indicator" id="step-3-label">3. Reader</div>
                <div style="color:var(--border)">→</div>
                <div class="step-indicator" id="step-4-label">4. Configure</div>
                <div style="color:var(--border)">→</div>
                <div class="step-indicator" id="step-5-label">5. Finish</div>
            </div>

            <div id="onboarding-content"></div>

            <style>
                .step-indicator { font-size:11px;font-weight:700;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.1em; }
                .step-indicator.active { color:var(--primary); }
                .proto-card {
                    padding:24px;border:1px solid var(--border);border-radius:16px;cursor:pointer;
                    transition:var(--transition);text-align:center;background:rgba(255,255,255,0.02);
                }
                .proto-card:hover { border-color:var(--primary);transform:translateY(-4px);background:rgba(124,133,255,0.05); }
                .proto-card.selected { border-width:2px;border-color:var(--primary);background:rgba(124,133,255,0.1);box-shadow:0 0 20px var(--primary-glow); }
                .reader-badge { display:inline-block;padding:3px 8px;border-radius:4px;font-size:10px;font-weight:700;
                    background:rgba(124,133,255,0.15);color:var(--primary);margin-bottom:6px; }
            </style>
        `;
    },

    async init() {
        this._qubes = await API.getQubes();
        if (this._qubes.length > 0) this.state.qubeId = this._qubes[0].id;
        await this.goToStep(1);
    },

    async goToStep(step) {
        this.state.step = step;
        for (let i = 1; i <= 5; i++) {
            document.getElementById(`step-${i}-label`)?.classList.toggle('active', i <= step);
        }
        const content = document.getElementById('onboarding-content');
        switch (step) {
            case 1: await this.renderStep1(content); break;
            case 2: await this.renderStep2(content); break;
            case 3: await this.renderStep3(content); break;
            case 4: await this.renderStep4(content); break;
            case 5: await this.renderStep5(content); break;
        }
    },

    // ── Step 1: Protocol ─────────────────────────────────────────────────────

    async renderStep1(container) {
        const protocols = await API.getProtocols();
        const qubes = this._qubes || [];

        const qubeSelector = qubes.length > 1
            ? `<div class="form-group" style="max-width:320px;margin-bottom:20px;">
                    <label>Target Qube Device</label>
                    <select id="qube-selector-step1">
                        ${qubes.map(q => `<option value="${q.id}" ${q.id === this.state.qubeId ? 'selected' : ''}>${q.name || q.id}</option>`).join('')}
                    </select>
               </div>`
            : qubes.length === 0
            ? `<div class="badge badge-error" style="margin-bottom:16px;padding:10px 16px;">No Qube devices found — claim one from the Fleet page first.</div>`
            : `<div class="page-subtitle" style="margin-bottom:16px;font-size:12px;">Target Qube: <strong>${qubes[0].name || qubes[0].id}</strong></div>`;

        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 1: Select Communication Protocol</h3>
                ${qubeSelector}
                <div class="grid grid-3">
                    ${protocols.map(p => `
                        <div class="proto-card" data-id="${p.id}" data-standard="${p.reader_standard}">
                            <div style="font-size:28px;margin-bottom:10px;">${this._protoIcon(p.id)}</div>
                            <div class="reader-badge">${p.reader_standard === 'multi_target' ? 'Shared Reader' : 'Per-Endpoint'}</div>
                            <div style="font-weight:700;font-size:15px;">${p.label}</div>
                            <div class="page-subtitle" style="font-size:11px;margin-top:6px;">${p.description}</div>
                        </div>
                    `).join('')}
                </div>
            </div>
        `;

        document.getElementById('qube-selector-step1')?.addEventListener('change', e => {
            this.state.qubeId = e.target.value;
        });
        container.querySelectorAll('.proto-card').forEach(card => {
            card.onclick = async () => {
                const proto = protocols.find(p => p.id === card.dataset.id);
                this.state.protocol = proto;
                this.state.isMultiTarget = proto.reader_standard === 'multi_target';
                await this.goToStep(2);
            };
        });
    },

    // ── Step 2: Device Template ───────────────────────────────────────────────

    async renderStep2(container) {
        const templates = await API.getDeviceTemplates(this.state.protocol.id);
        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 2: Select Device Template</h3>
                <p class="page-subtitle" style="margin-bottom:18px;">
                    Protocol: <strong>${this.state.protocol.label}</strong>
                    <span class="badge ${this.state.isMultiTarget ? 'badge-blue' : 'badge-success'}" style="margin-left:8px;">
                        ${this.state.isMultiTarget ? 'Shared Reader' : 'Per-Endpoint Reader'}
                    </span>
                </p>
                ${templates.length === 0
                    ? `<div class="page-subtitle" style="padding:32px 0;text-align:center;">
                          No templates found for ${this.state.protocol.label}.<br>
                          <a href="#templates" style="color:var(--primary);">Create a device template first.</a>
                       </div>`
                    : `<div class="grid grid-3">
                        ${templates.map(t => `
                            <div class="proto-card" data-id="${t.id}">
                                <div style="font-size:26px;margin-bottom:8px;">${this._protoIcon(t.protocol)}</div>
                                <div style="font-weight:700;font-size:14px;">${[t.manufacturer, t.model].filter(Boolean).join(' ') || t.name}</div>
                                <div class="page-subtitle" style="font-size:11px;margin-top:4px;">${t.name}</div>
                                <div style="font-size:10px;color:var(--text-dim);margin-top:6px;">${(t.sensor_config?.[PROTO_ARRAY_KEY[t.protocol]] || []).length} measurements</div>
                            </div>
                        `).join('')}
                       </div>`}
                <div class="mt-20"><button class="btn btn-ghost" id="btn-back">← Back</button></div>
            </div>
        `;

        container.querySelectorAll('.proto-card').forEach(card => {
            card.onclick = async () => {
                this.state.deviceTemplate = templates.find(t => t.id === card.dataset.id);
                await this.goToStep(3);
            };
        });
        document.getElementById('btn-back').onclick = () => this.goToStep(1);
    },

    // ── Step 3: Reader Configuration ──────────────────────────────────────────
    //
    // multi_target (SNMP, HTTP):
    //   - Show the existing shared reader if any, or "will auto-create" message.
    //   - NO connection form needed — user doesn't configure the reader.
    //
    // endpoint (Modbus, MQTT, OPC-UA):
    //   - Show existing readers with matching protocol.
    //   - User can select one (reuse) or define a new connection.

    async renderStep3(container) {
        if (!this.state.qubeId) {
            container.innerHTML = `<div class="card text-center"><p class="page-subtitle">No Qube selected. <a href="#fleet">Go to Fleet</a> to claim one.</p></div>`;
            return;
        }

        const readers = await API.getQubeReaders(this.state.qubeId);
        const protoReaders = readers.filter(r => r.protocol === this.state.protocol.id);

        const rTemplates = await API.getReaderTemplates(this.state.protocol.id);
        this.state.readerTemplate = rTemplates[0] || null;
        this.state.existingReaders = protoReaders;
        this.state.selectedReaderId = null;
        this.state.readerConnValues = {};

        if (this.state.isMultiTarget) {
            // Multi-target: one shared reader per Qube — just show status
            const sharedReader = protoReaders[0] || null;
            container.innerHTML = `
                <div class="card">
                    <h3 class="card-title">Step 3: Reader — ${this.state.protocol.label} (Shared)</h3>
                    <div style="background:rgba(99,179,237,0.08);border:1px solid rgba(99,179,237,0.25);border-radius:12px;padding:20px;margin-bottom:20px;">
                        <div style="font-size:13px;font-weight:600;margin-bottom:8px;">
                            ${this.state.protocol.label} uses a <strong>shared multi-target reader</strong>
                        </div>
                        <p class="page-subtitle" style="font-size:12px;margin:0;">
                            One container handles all ${this.state.protocol.label} devices on this Qube.
                            ${sharedReader
                                ? `An existing reader <strong>${sharedReader.name}</strong> will be used automatically.`
                                : `No reader exists yet — one will be created automatically when you add this sensor.`}
                        </p>
                    </div>
                    ${sharedReader ? `
                        <div style="display:flex;gap:12px;align-items:center;padding:12px 16px;border:1px solid var(--border);border-radius:8px;">
                            <span class="badge badge-success">Active</span>
                            <div>
                                <div style="font-weight:600;font-size:13px;">${sharedReader.name}</div>
                                <div style="font-size:11px;color:var(--text-dim);">Sensors on this reader: ${sharedReader.sensor_count ?? '?'}</div>
                            </div>
                        </div>
                    ` : `
                        <div style="color:var(--text-dim);font-size:12px;padding:12px 0;">
                            A new ${this.state.protocol.label} reader container will be deployed automatically.
                        </div>
                    `}
                    <div class="mt-20 flex-between">
                        <button class="btn btn-ghost" id="btn-back">← Back</button>
                        <button class="btn btn-primary" id="btn-next">Continue →</button>
                    </div>
                </div>
            `;
            document.getElementById('btn-back').onclick = () => this.goToStep(2);
            document.getElementById('btn-next').onclick = () => this.goToStep(4);
            return;
        }

        // Endpoint protocol: show existing readers + optional new connection form
        const schema = this.state.readerTemplate?.connection_schema || { type: 'object', properties: {} };

        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 3: Reader — ${this.state.protocol.label} Connection</h3>
                <p class="page-subtitle" style="margin-bottom:18px;">
                    Each unique endpoint (host:port / broker / server URL) gets its own reader container.
                    Sensors sharing the same endpoint share one container.
                </p>

                ${protoReaders.length > 0 ? `
                    <div class="section-label" style="margin-bottom:10px;">Existing Readers on This Qube</div>
                    <div class="grid grid-2" style="margin-bottom:20px;">
                        ${protoReaders.map(rd => `
                            <div class="proto-card reader-select-card" data-id="${rd.id}">
                                <div style="font-weight:700;font-size:13px;">${rd.name}</div>
                                <div style="font-size:11px;color:var(--text-dim);margin-top:4px;">
                                    ${rd.config_json?.host || rd.config_json?.broker_host || rd.config_json?.endpoint || 'No endpoint'}
                                    ${rd.config_json?.port ? ':' + rd.config_json.port : ''}
                                </div>
                                <div style="font-size:10px;color:var(--text-dim);margin-top:2px;">
                                    ${rd.sensor_count ?? 0} sensor(s)
                                </div>
                            </div>
                        `).join('')}
                    </div>
                    <div style="text-align:center;color:var(--text-dim);font-size:12px;margin-bottom:12px;">— or define a new endpoint —</div>
                ` : ''}

                <div id="new-reader-form-wrap">
                    <div class="section-label" style="margin-bottom:10px;">New Connection</div>
                    <div id="new-reader-form"></div>
                </div>

                <div class="mt-20 flex-between">
                    <button class="btn btn-ghost" id="btn-back">← Back</button>
                    <button class="btn btn-primary" id="btn-next">Continue →</button>
                </div>
            </div>
        `;

        Components.renderSchemaForm(schema, 'new-reader-form', 'rdr');

        container.querySelectorAll('.reader-select-card').forEach(card => {
            card.onclick = () => {
                container.querySelectorAll('.reader-select-card').forEach(c => c.classList.remove('selected'));
                if (this.state.selectedReaderId === card.dataset.id) {
                    // Toggle off — deselect to create new
                    this.state.selectedReaderId = null;
                    document.getElementById('new-reader-form-wrap').style.display = '';
                } else {
                    card.classList.add('selected');
                    this.state.selectedReaderId = card.dataset.id;
                    document.getElementById('new-reader-form-wrap').style.display = 'none';
                }
            };
        });

        document.getElementById('btn-back').onclick = () => this.goToStep(2);
        document.getElementById('btn-next').onclick = () => {
            if (!this.state.selectedReaderId) {
                // Collect connection form values
                this.state.readerConnValues = Components.collectSchemaValues(schema, 'rdr');
            }
            this.goToStep(4);
        };
    },

    // ── Step 4: Device Configuration ─────────────────────────────────────────

    async renderStep4(container) {
        const proto = this.state.protocol.id;
        const arrayKey = PROTO_ARRAY_KEY[proto] || 'entries';
        const measurements = this.state.deviceTemplate.sensor_config?.[arrayKey] || [];

        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 4: Device Configuration</h3>
                <div class="grid grid-2">
                    <div>
                        <div class="form-group">
                            <label>Sensor Name <span style="color:var(--error)">*</span></label>
                            <input type="text" id="sensor-name" placeholder="e.g. Rack_A_UPS_01" value="${this.state.deviceTemplate.name || ''}">
                        </div>
                        <div id="sensor-params-form"></div>
                    </div>
                    <div>
                        <div class="flex-between" style="margin-bottom:10px;">
                            <div class="section-label">Defined Measurements (${measurements.length})</div>
                            <button id="btn-toggle-advanced" class="btn btn-ghost btn-sm">Advanced Edit</button>
                        </div>
                        <div id="config-ui-container" style="background:rgba(0,0,0,0.2);padding:16px;border-radius:12px;border:1px solid var(--border);">
                            <div id="fields-list"></div>
                        </div>
                    </div>
                </div>
                <div class="mt-20 flex-between">
                    <button class="btn btn-ghost" id="btn-back">← Back</button>
                    <button class="btn btn-primary" id="btn-next">Review →</button>
                </div>
            </div>
        `;

        Components.renderSchemaForm(this.state.deviceTemplate.sensor_params_schema, 'sensor-params-form', 'sns');
        this._renderFieldsList(measurements);

        document.getElementById('btn-toggle-advanced').onclick = () => {
            this.state.advancedMode = !this.state.advancedMode;
            if (this.state.advancedMode) {
                document.getElementById('config-ui-container').innerHTML =
                    `<textarea id="json-editor-area" style="width:100%;height:280px;font-family:var(--font-mono);font-size:11px;background:#1a1d32;box-sizing:border-box;">${JSON.stringify(this.state.deviceTemplate.sensor_config, null, 2)}</textarea>`;
            } else {
                document.getElementById('config-ui-container').innerHTML = `<div id="fields-list"></div>`;
                this._renderFieldsList(measurements);
            }
        };

        document.getElementById('btn-back').onclick = () => this.goToStep(3);
        document.getElementById('btn-next').onclick = () => {
            if (this.state.advancedMode) {
                try {
                    this.state.customSensorConfig = JSON.parse(document.getElementById('json-editor-area').value);
                } catch {
                    Components.showAlert('Invalid JSON in advanced editor', 'error');
                    return;
                }
            }
            this.goToStep(5);
        };
    },

    _renderFieldsList(measurements) {
        const list = document.getElementById('fields-list');
        if (!list) return;
        if (!measurements.length) {
            list.innerHTML = '<div class="page-subtitle" style="font-size:12px;">No measurements defined.</div>';
            return;
        }
        list.innerHTML = measurements.map(f =>
            `<div style="display:flex;justify-content:space-between;padding:4px 0;font-size:12px;border-bottom:1px solid rgba(255,255,255,0.04);">
                <span style="color:var(--text-main);">${f.field_key || f.oid || f.node_id || '—'}</span>
                <span class="badge" style="font-size:9px;padding:2px 6px;">${f.unit || '-'}</span>
             </div>`
        ).join('');
    },

    // ── Step 5: Review & Confirm ──────────────────────────────────────────────

    async renderStep5(container) {
        const proto = this.state.protocol;
        const tmpl = this.state.deviceTemplate;
        const readerInfo = this.state.selectedReaderId
            ? `Reuse <strong>${this.state.existingReaders.find(r => r.id === this.state.selectedReaderId)?.name || this.state.selectedReaderId}</strong>`
            : this.state.isMultiTarget
                ? 'Auto-assign to shared reader'
                : 'Create new reader container';

        container.innerHTML = `
            <div class="card text-center">
                <div style="font-size:56px;margin-bottom:20px;">🎯</div>
                <h2 class="page-title">Ready to Sync</h2>
                <p class="page-subtitle mb-20" style="margin-bottom:28px;">Review and confirm. The Qube config will update automatically.</p>

                <div style="text-align:left;max-width:500px;margin:0 auto;background:rgba(124,133,255,0.03);padding:28px;border-radius:16px;border:1px solid var(--primary-glow);">
                    <div class="flex-between" style="margin-bottom:10px;"><span>Protocol</span><span class="badge badge-blue">${proto.label}</span></div>
                    <div class="flex-between" style="margin-bottom:10px;"><span>Template</span><b>${tmpl.name}</b></div>
                    <div class="flex-between" style="margin-bottom:10px;"><span>Device</span><b>${[tmpl.manufacturer, tmpl.model].filter(Boolean).join(' ') || '—'}</b></div>
                    <div class="flex-between" style="margin-bottom:10px;"><span>Reader</span><span>${readerInfo}</span></div>
                    <div class="flex-between"><span>Reader Mode</span><span class="badge ${proto.reader_standard === 'multi_target' ? 'badge-blue' : 'badge-success'}">${proto.reader_standard}</span></div>
                </div>

                <div class="mt-20 flex" style="justify-content:center;gap:12px;">
                    <button class="btn btn-ghost" id="btn-back">Back</button>
                    <button class="btn btn-primary" id="btn-finish" style="padding:14px 48px;">CONFIRM & SYNC</button>
                </div>
            </div>
        `;

        document.getElementById('btn-back').onclick = () => this.goToStep(4);
        document.getElementById('btn-finish').onclick = () => this.handleFinish();
    },

    // ── Finish ────────────────────────────────────────────────────────────────

    async handleFinish() {
        const btn = document.getElementById('btn-finish');
        try {
            btn.disabled = true;
            btn.textContent = 'SYNCING...';

            const sensorName = document.getElementById('sensor-name')?.value.trim()
                || `${this.state.deviceTemplate.name}_Sensor`;
            const sensorParams = Components.collectSchemaValues(this.state.deviceTemplate.sensor_params_schema, 'sns');

            if (this.state.selectedReaderId) {
                // User explicitly selected an existing reader → use classic endpoint
                await API.post(`/api/v1/readers/${this.state.selectedReaderId}/sensors`, {
                    name: sensorName,
                    template_id: this.state.deviceTemplate.id,
                    params: sensorParams,
                    output: 'influxdb',
                    tags_json: {},
                    table_name: 'Measurements',
                });
            } else {
                // Use smart endpoint — backend auto-finds/creates the reader
                await API.post(`/api/v1/qubes/${this.state.qubeId}/sensors`, {
                    name: sensorName,
                    template_id: this.state.deviceTemplate.id,
                    params: sensorParams,
                    reader_config: this.state.isMultiTarget ? {} : this.state.readerConnValues,
                    reader_name: this.state.isMultiTarget
                        ? `${this.state.protocol.label} Reader`
                        : (this.state.readerConnValues?.host || this.state.readerConnValues?.broker_host || this.state.protocol.label) + ' Reader',
                    output: 'influxdb',
                    tags_json: {},
                    table_name: 'Measurements',
                });
            }

            Components.showAlert('Device added and edge sync triggered!', 'success');
            window.location.hash = '#dashboard';
        } catch (err) {
            Components.showAlert(err.message, 'error');
            btn.disabled = false;
            btn.textContent = 'CONFIRM & SYNC';
        }
    },

    // ── Helpers ───────────────────────────────────────────────────────────────

    _protoIcon(id) {
        const icons = { modbus_tcp: '⚡', snmp: '🌐', mqtt: '📡', opcua: '🏭', http: '🔗', bacnet: '🏢', lorawan: '📶', dnp3: '🔌' };
        return icons[id] || '🔧';
    },
};

export default Onboarding;
