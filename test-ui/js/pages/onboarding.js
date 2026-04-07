import API from '../api.js';
import Components from '../components.js';

const Onboarding = {
    state: {
        step: 1,
        qubeId: '',
        protocol: null,
        deviceTemplate: null,
        readerTemplate: null,
        existingReaders: [],
        selectedReaderId: null,
        isMultiTarget: false,
        advancedMode: false,
        customConfig: null
    },

    async render() {
        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">Device Onboarding Wizard</h1>
                    <p class="page-subtitle">Configure a new physical device in 5 simple steps</p>
                </div>
            </div>

            <!-- Progress Bar -->
            <div class="flex-between mb-20" style="margin-bottom: 30px; background: var(--bg-sidebar); padding: 20px; border-radius: 12px; border: 1px solid var(--border);">
                <div class="step-indicator ${this.state.step >= 1 ? 'active' : ''}" id="step-1-label">1. Protocol</div>
                <div style="color: var(--border)">→</div>
                <div class="step-indicator ${this.state.step >= 2 ? 'active' : ''}" id="step-2-label">2. Template</div>
                <div style="color: var(--border)">→</div>
                <div class="step-indicator ${this.state.step >= 3 ? 'active' : ''}" id="step-3-label">3. Reader</div>
                <div style="color: var(--border)">→</div>
                <div class="step-indicator ${this.state.step >= 4 ? 'active' : ''}" id="step-4-label">4. Configure</div>
                <div style="color: var(--border)">→</div>
                <div class="step-indicator ${this.state.step >= 5 ? 'active' : ''}" id="step-5-label">5. Finish</div>
            </div>

            <div id="onboarding-content"></div>

            <style>
                .step-indicator { font-size: 11px; font-weight: 700; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.1em; }
                .step-indicator.active { color: var(--primary); }
                .proto-card { 
                    padding: 24px; border: 1px solid var(--border); border-radius: 16px; cursor: pointer; transition: var(--transition);
                    text-align: center; background: rgba(255,255,255,0.02);
                }
                .proto-card:hover { border-color: var(--primary); transform: translateY(-4px); background: rgba(124, 133, 255, 0.05); }
                .proto-card.selected { border-width: 2px; border-color: var(--primary); background: rgba(124, 133, 255, 0.1); box-shadow: 0 0 20px var(--primary-glow); }
            </style>
        `;
    },

    async init() {
        const qubes = await API.getQubes();
        if (qubes.length > 0) this.state.qubeId = qubes[0].id;
        await this.goToStep(1);
    },

    async goToStep(step) {
        this.state.step = step;
        const content = document.getElementById('onboarding-content');
        
        for(let i=1; i<=5; i++) {
            const el = document.getElementById(`step-${i}-label`);
            if (el) el.classList.toggle('active', i <= step);
        }

        switch(step) {
            case 1: await this.renderStep1(content); break;
            case 2: await this.renderStep2(content); break;
            case 3: await this.renderStep3(content); break;
            case 4: await this.renderStep4(content); break;
            case 5: await this.renderStep5(content); break;
        }
    },

    async renderStep1(container) {
        const protocols = await API.getProtocols();
        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 1: Select Communication Protocol</h3>
                <div class="grid grid-3">
                    ${protocols.map(p => `
                        <div class="proto-card" data-id="${p.id}">
                            <div style="font-size: 32px; margin-bottom: 12px;">🔌</div>
                            <div style="font-weight: 700; font-size: 16px;">${p.label}</div>
                            <div class="page-subtitle" style="font-size: 12px; margin-top: 8px;">${p.description}</div>
                        </div>
                    `).join('')}
                </div>
            </div>
        `;

        container.querySelectorAll('.proto-card').forEach(card => {
            card.onclick = async () => {
                this.state.protocol = protocols.find(p => p.id === card.dataset.id);
                this.state.isMultiTarget = this.state.protocol.reader_standard === 'multi_target';
                await this.goToStep(2);
            };
        });
    },

    async renderStep2(container) {
        const templates = await API.getDeviceTemplates(this.state.protocol.id);
        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 2: Select Device Template</h3>
                <div class="grid grid-3">
                    ${templates.map(t => `
                        <div class="proto-card" data-id="${t.id}">
                            <div style="font-size: 32px; margin-bottom: 12px;">📟</div>
                            <div style="font-weight: 700; font-size: 16px;">${t.manufacturer} ${t.model}</div>
                            <div class="page-subtitle" style="font-size: 12px;">${t.name}</div>
                        </div>
                    `).join('')}
                </div>
                <div class="mt-20">
                    <button class="btn btn-ghost" id="btn-back">← Back</button>
                </div>
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

    async renderStep3(container) {
        const readers = await API.getQubeReaders(this.state.qubeId);
        const rTemplates = await API.getReaderTemplates(this.state.protocol.id);
        this.state.readerTemplate = rTemplates[0] || { name: 'Generic Reader', connection_schema: { type: 'object', properties: {} } };

        this.state.existingReaders = readers.filter(r => r.protocol === this.state.protocol.id);

        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 3: Edge Reader Configuration</h3>
                <p class="page-subtitle mb-20" style="margin-bottom: 24px;">
                    ${this.state.isMultiTarget 
                        ? 'This protocol shares a single container. We will use the existing one if available.' 
                        : 'Connections can be shared. Select an endpoint below or define a new connection host.'}
                </p>

                <div id="reader-options">
                    ${this.state.existingReaders.length > 0 ? `
                        <label>Detected Compatible Readers</label>
                        <div class="grid grid-2 mb-20" style="margin-bottom: 24px;">
                            ${this.state.existingReaders.map(r => `
                                <div class="proto-card reader-select-card" data-id="${r.id}">
                                    <div style="font-weight: 700;">${r.name}</div>
                                    <div class="page-subtitle" style="font-size: 11px;">Endpoint: ${r.config_json.host || r.config_json.endpoint || 'N/A'}</div>
                                </div>
                            `).join('')}
                        </div>
                    ` : ''}
                    
                    <div id="new-reader-toggle" class="section-label" style="cursor:pointer; color: var(--primary);">
                        ${this.state.existingReaders.length > 0 ? '+ Add New Connection Endpoint' : 'Configure Connection'}
                    </div>
                    <div id="new-reader-form" class="${this.state.existingReaders.length > 0 ? 'hidden' : ''}"></div>
                </div>

                <div class="mt-20 flex-between">
                    <button class="btn btn-ghost" id="btn-back">← Back</button>
                    <button class="btn btn-primary" id="btn-next">Continue →</button>
                </div>
            </div>
        `;

        Components.renderSchemaForm(this.state.readerTemplate.connection_schema, 'new-reader-form', 'rdr');

        document.getElementById('new-reader-toggle').onclick = () => {
            document.getElementById('new-reader-form').classList.toggle('hidden');
            this.state.selectedReaderId = null;
            container.querySelectorAll('.reader-select-card').forEach(c => c.classList.remove('selected'));
        };

        container.querySelectorAll('.reader-select-card').forEach(card => {
            card.onclick = () => {
                container.querySelectorAll('.reader-select-card').forEach(c => c.classList.remove('selected'));
                card.classList.add('selected');
                this.state.selectedReaderId = card.dataset.id;
                document.getElementById('new-reader-form').classList.add('hidden');
            };
        });

        document.getElementById('btn-back').onclick = () => this.goToStep(2);
        document.getElementById('btn-next').onclick = () => this.goToStep(4);
    },

    async renderStep4(container) {
        container.innerHTML = `
            <div class="card">
                <h3 class="card-title">Step 4: Device Configuration</h3>
                <div class="grid grid-2">
                    <div>
                        <div class="form-group">
                            <label>Sensor Name</label>
                            <input type="text" id="sensor-name" placeholder="e.g. Rack_A_PM5100">
                        </div>
                        <div id="sensor-params-form"></div>
                    </div>
                    <div>
                        <div class="flex-between">
                            <div class="section-label">Defined Measurements</div>
                            <button id="btn-toggle-advanced" class="btn btn-ghost btn-sm">🛠️ Advanced Mode</button>
                        </div>
                        <div id="config-ui-container" style="background: rgba(0,0,0,0.2); padding: 16px; border-radius: 12px; border: 1px solid var(--border); margin-top: 12px;">
                            <div id="fields-list" style="font-size: 12px; color: var(--text-dim);"></div>
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
        this.updateFieldsList();

        document.getElementById('btn-toggle-advanced').onclick = () => {
            this.state.advancedMode = !this.state.advancedMode;
            this.updateConfigUI();
        };

        document.getElementById('btn-back').onclick = () => this.goToStep(3);
        document.getElementById('btn-next').onclick = () => this.goToStep(5);
    },

    updateFieldsList() {
        const list = document.getElementById('fields-list');
        const config = this.state.deviceTemplate.sensor_config;
        const key = this.state.protocol.id === 'modbus_tcp' ? 'registers' : this.state.protocol.id === 'snmp' ? 'oids' : 'nodes';
        const fields = config[key] || [];
        list.innerHTML = fields.map(f => `<div>✓ ${f.field_key} <span class="badge" style="font-size: 9px; padding: 2px 6px;">${f.unit || '-'}</span></div>`).join('');
    },

    updateConfigUI() {
        const container = document.getElementById('config-ui-container');
        if (this.state.advancedMode) {
            Components.renderJsonEditor(this.state.deviceTemplate.sensor_config, 'config-ui-container', 'Raw Sensor Config');
        } else {
            container.innerHTML = `<div id="fields-list" style="font-size: 12px; color: var(--text-dim);"></div>`;
            this.updateFieldsList();
        }
    },

    async renderStep5(container) {
        container.innerHTML = `
            <div class="card text-center">
                <div style="font-size: 64px; margin-bottom: 24px;">🎯</div>
                <h2 class="page-title">Ready to Sync</h2>
                <p class="page-subtitle mb-20" style="margin-bottom: 32px;">Please review the summary below. Upon clicking confirm, the edge gateway will be automatically updated.</p>
                
                <div style="text-align: left; max-width: 480px; margin: 0 auto; background: rgba(124, 133, 255, 0.03); padding: 32px; border-radius: 20px; border: 1px solid var(--primary-glow);">
                    <div class="flex-between"><span>Protocol</span> <span class="badge badge-blue">${this.state.protocol.label}</span></div>
                    <div class="flex-between mt-10"><span>Reader Mode</span> <b>${this.state.selectedReaderId ? 'Reuse Existing' : 'Deploy New Container'}</b></div>
                    <div class="flex-between mt-10"><span>Target Device</span> <b>${this.state.deviceTemplate.manufacturer} ${this.state.deviceTemplate.model}</b></div>
                </div>

                <div class="mt-20 flex" style="justify-content: center;">
                    <button class="btn btn-ghost" id="btn-back">Back</button>
                    <button class="btn btn-primary" id="btn-finish" style="padding: 14px 48px;">🚀 CONFIRM & SYNC</button>
                </div>
            </div>
        `;

        document.getElementById('btn-back').onclick = () => this.goToStep(4);
        document.getElementById('btn-finish').onclick = () => this.handleFinish();
    },

    async handleFinish() {
        const btn = document.getElementById('btn-finish');
        try {
            btn.disabled = true;
            btn.textContent = 'SYNCING...';

            let readerId = this.state.selectedReaderId;
            
            // 1. Create reader if new
            if (!readerId) {
                const connValues = Components.collectSchemaValues(this.state.readerTemplate.connection_schema, 'rdr');
                const readerRes = await API.createReader(this.state.qubeId, {
                    name: `${this.state.protocol.label}_Reader_${Date.now().toString().slice(-4)}`,
                    protocol: this.state.protocol.id,
                    template_id: this.state.readerTemplate.id,
                    config_json: connValues
                });
                readerId = readerRes.reader_id;
            }

            // 2. Create sensor
            const sensorParams = Components.collectSchemaValues(this.state.deviceTemplate.sensor_params_schema, 'sns');
            const sensorName = document.getElementById('sensor-name').value || `${this.state.deviceTemplate.name}_Sensor`;
            
            let sensorConfig = this.state.deviceTemplate.sensor_config;
            if (this.state.advancedMode) {
                sensorConfig = JSON.parse(document.getElementById('json-editor-area').value);
            }

            await API.post(`/api/v1/readers/${readerId}/sensors`, {
                name: sensorName,
                template_id: this.state.deviceTemplate.id,
                params: sensorParams,
                output: "influxdb",
                tags_json: { location: "Management Center" },
                table_name: "Measurements"
            });

            Components.showAlert('Device successfully added and edge sync triggered!', 'success');
            window.location.hash = '#dashboard';
        } catch (err) {
            Components.showAlert(err.message, 'error');
            btn.disabled = false;
            btn.textContent = '🚀 CONFIRM & SYNC';
        }
    }
};

export default Onboarding;
