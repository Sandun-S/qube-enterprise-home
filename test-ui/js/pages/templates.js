import API from '../api.js';
import Components from '../components.js';

const Templates = {
    _editState: null,
    _protocols: [],
    _protocolMap: {},   // id → protocol object (includes icon, sensor_config_key, measurement_fields_schema, default_params_schema)

    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Device Templates</h2>
                    <p class="page-subtitle">Standardized measurement definitions for physical devices</p>
                </div>
                <button class="btn btn-primary" id="btn-create-template">+ Create New Template</button>
            </div>

            <div class="card">
                <div class="flex-between mb-20">
                    <div class="flex" style="gap:8px;align-items:center;">
                        <span class="badge badge-blue">Global</span>
                        <span class="badge badge-success">Organization</span>
                    </div>
                    <select id="template-proto-filter" style="width:160px;">
                        <option value="">All Protocols</option>
                    </select>
                </div>
                <div id="templates-grid" class="grid grid-3">
                    <div class="text-center page-subtitle">Loading device catalog...</div>
                </div>
            </div>

            <!-- Modal overlay -->
            <div id="tmpl-overlay" style="display:none;position:fixed;inset:0;background:rgba(0,0,0,0.78);z-index:1000;overflow-y:auto;padding:32px 20px;">
                <div id="tmpl-modal" style="background:var(--bg-card);border:1px solid var(--border);border-radius:20px;width:100%;max-width:900px;margin:0 auto;padding:36px;position:relative;">
                    <button id="btn-close-modal" style="position:absolute;top:18px;right:22px;background:none;border:none;color:var(--text-dim);font-size:22px;cursor:pointer;line-height:1;padding:0;">✕</button>
                    <div id="modal-body"></div>
                </div>
            </div>

            <style>
                .meas-input {
                    font-size:12px;padding:5px 8px;background:#1e2133;border:1px solid var(--border);
                    border-radius:6px;color:var(--text-main);width:100%;box-sizing:border-box;min-width:60px;
                }
                .param-row { display:grid;grid-template-columns:1fr 1.2fr 100px 80px 36px;gap:8px;margin-bottom:8px;align-items:end; }
            </style>
        `;
    },

    async init() {
        this._protocols = await API.getProtocols();
        this._protocolMap = {};
        this._protocols.forEach(p => { this._protocolMap[p.id] = p; });

        const filter = document.getElementById('template-proto-filter');
        this._protocols.forEach(p => {
            const opt = document.createElement('option');
            opt.value = p.id;
            opt.textContent = p.label;
            filter.appendChild(opt);
        });

        await this.loadTemplates();
        filter.addEventListener('change', () => this.loadTemplates(filter.value));

        document.getElementById('btn-create-template').onclick = () => this._showCreateModal();
        document.getElementById('btn-close-modal').onclick = () => this._closeModal();
        document.getElementById('tmpl-overlay').onclick = (e) => {
            if (e.target === document.getElementById('tmpl-overlay')) this._closeModal();
        };
    },

    _openModal() {
        document.getElementById('tmpl-overlay').style.display = 'block';
    },
    _closeModal() {
        document.getElementById('tmpl-overlay').style.display = 'none';
        this._editState = null;
    },

    // ── Template Cards ─────────────────────────────────────────────────────────

    async loadTemplates(protocol = '') {
        try {
            const templates = await API.getDeviceTemplates(protocol);
            const grid = document.getElementById('templates-grid');
            grid.innerHTML = '';

            // Raw JSON panel
            const raw = document.createElement('div');
            raw.className = 'card';
            raw.style.gridColumn = '1 / -1';
            raw.innerHTML = `
                <div class="flex-between">
                    <h3 class="card-title">Device Catalog Data</h3>
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-raw">Toggle Raw JSON</button>
                </div>
                <pre id="templates-raw-json" class="raw-json-preview hidden">${JSON.stringify(templates, null, 2)}</pre>
            `;
            grid.appendChild(raw);
            document.getElementById('btn-toggle-raw').onclick = () => {
                document.getElementById('templates-raw-json').classList.toggle('hidden');
            };

            if (templates.length === 0) {
                const empty = document.createElement('div');
                empty.style.cssText = 'grid-column:1/-1;text-align:center;padding:48px;color:var(--text-dim);';
                empty.textContent = 'No templates found. Click "Create New Template" to add one.';
                grid.appendChild(empty);
                return;
            }

            templates.forEach(t => {
                const arrayKey = this._protocolMap[t.protocol]?.sensor_config_key || 'entries';
                const entries = t.sensor_config?.[arrayKey] || [];
                const card = document.createElement('div');
                card.className = 'card';
                card.style.position = 'relative';
                card.innerHTML = `
                    <div style="position:absolute;top:14px;right:16px;">
                        <span class="badge badge-${t.is_global ? 'blue' : 'success'}" style="font-size:9px;">${t.is_global ? 'GLOBAL' : 'ORG'}</span>
                    </div>
                    <div style="font-size:11px;color:var(--text-dim);margin-bottom:4px;">${this._protocolMap[t.protocol]?.icon || '🔧'} ${t.protocol}</div>
                    <div style="font-weight:700;margin-bottom:2px;padding-right:56px;">${[t.manufacturer, t.model].filter(Boolean).join(' ') || '—'}</div>
                    <div style="font-size:13px;font-weight:600;color:var(--primary);margin-bottom:8px;">${t.name}</div>
                    <p class="page-subtitle" style="font-size:11px;margin-bottom:10px;height:3.2em;overflow:hidden;line-height:1.6;">${t.description || ''}</p>
                    <div style="font-size:11px;color:var(--text-dim);margin-bottom:12px;">
                        ${entries.length} measurement${entries.length !== 1 ? 's' : ''}
                        ${t.sensor_params_schema?.properties ? ' · ' + Object.keys(t.sensor_params_schema.properties).length + ' device params' : ''}
                    </div>
                    <div class="flex-between" style="border-top:1px solid var(--border);padding-top:10px;">
                        <button class="btn btn-ghost btn-sm btn-view-json" data-id="${t.id}" data-tmpl='${JSON.stringify({id:t.id,sensor_config:t.sensor_config,sensor_params_schema:t.sensor_params_schema}).replace(/'/g,"&#39;")}'>JSON</button>
                        <button class="btn btn-ghost btn-sm btn-edit-template" data-id="${t.id}">Edit</button>
                    </div>
                `;
                grid.appendChild(card);
            });

            document.querySelectorAll('.btn-edit-template').forEach(btn => {
                btn.onclick = () => this._showEditModal(btn.dataset.id);
            });

            document.querySelectorAll('.btn-view-json').forEach(btn => {
                btn.onclick = () => {
                    try {
                        const d = JSON.parse(btn.dataset.tmpl.replace(/&#39;/g, "'"));
                        this._showJsonViewModal(d);
                    } catch(e) { Components.showAlert('Could not parse template data', 'error'); }
                };
            });

        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    _showJsonViewModal(tmpl) {
        document.getElementById('modal-body').innerHTML = `
            <h3 style="margin-bottom:16px;">Template JSON Data</h3>
            <div class="section-label" style="margin-bottom:8px;">sensor_config</div>
            <pre class="raw-json-preview" style="max-height:300px;overflow-y:auto;">${JSON.stringify(tmpl.sensor_config, null, 2)}</pre>
            <div class="section-label" style="margin:16px 0 8px;">sensor_params_schema</div>
            <pre class="raw-json-preview" style="max-height:200px;overflow-y:auto;">${JSON.stringify(tmpl.sensor_params_schema, null, 2)}</pre>
            <div class="flex-between" style="margin-top:20px;">
                <div></div>
                <button class="btn btn-ghost" id="btn-back-to-list">Close</button>
            </div>
        `;
        document.getElementById('btn-back-to-list').onclick = () => this._closeModal();
        this._openModal();
    },

    // ── CREATE ─────────────────────────────────────────────────────────────────

    _showCreateModal() {
        this._editState = {
            mode: 'create',
            protocol: null,
            name: '', manufacturer: '', model: '', description: '',
            measurements: [],
            sensorParamsSchema: { type: 'object', properties: {}, required: [] },
        };
        this._openModal();
        this._renderProtocolStep();
    },

    _renderProtocolStep() {
        document.getElementById('modal-body').innerHTML = `
            <h2 style="font-size:20px;font-weight:700;margin-bottom:6px;">Create Device Template</h2>
            <p class="page-subtitle" style="margin-bottom:24px;">Select the communication protocol for this device type</p>
            <div class="grid grid-3" style="gap:14px;" id="proto-select-grid"></div>
        `;

        const grid = document.getElementById('proto-select-grid');
        this._protocols.forEach(p => {
            const card = document.createElement('div');
            card.style.cssText = 'padding:20px;border:1px solid var(--border);border-radius:12px;cursor:pointer;text-align:center;transition:var(--transition);';
            card.innerHTML = `
                <div style="font-size:30px;margin-bottom:8px;">${p.icon || '🔧'}</div>
                <div style="font-weight:700;font-size:14px;">${p.label}</div>
                <div style="font-size:11px;color:var(--text-dim);margin-top:6px;">${p.description || ''}</div>
            `;
            card.onmouseenter = () => { card.style.borderColor = 'var(--primary)'; card.style.background = 'rgba(124,133,255,0.06)'; };
            card.onmouseleave = () => { card.style.borderColor = 'var(--border)'; card.style.background = ''; };
            card.onclick = async () => {
                this._editState.protocol = p;
                // Load reader template to seed per-device params and show connection reference
                try {
                    const rts = await API.getReaderTemplates(p.id);
                    this._editState.readerTemplate = rts[0] || null;
                } catch (e) {
                    this._editState.readerTemplate = null;
                }
                this._editState.sensorParamsSchema = p.default_params_schema
                    ? JSON.parse(JSON.stringify(p.default_params_schema))
                    : { type: 'object', properties: {}, required: [] };
                this._renderMainForm();
            };
            grid.appendChild(card);
        });
    },

    // ── Main Form (Create & Edit) ───────────────────────────────────────────────

    _renderMainForm() {
        const s = this._editState;
        const isEdit = s.mode === 'edit';
        const proto = s.protocol;
        const fields = proto.measurement_fields_schema || [];
        const arrayKey = proto.sensor_config_key || 'entries';

        document.getElementById('modal-body').innerHTML = `
            <div style="display:flex;align-items:center;gap:14px;margin-bottom:24px;">
                ${!isEdit ? '<button id="btn-back-proto" class="btn btn-ghost btn-sm">← Protocol</button>' : ''}
                <div>
                    <h2 style="font-size:20px;font-weight:700;margin:0;">${isEdit ? 'Edit' : 'New'} Device Template</h2>
                    <div style="margin-top:4px;display:flex;gap:8px;align-items:center;">
                        <span class="badge badge-blue">${proto.label}</span>
                        ${isEdit ? `<span class="badge badge-${s.is_global ? 'blue' : 'success'}" style="font-size:9px;">${s.is_global ? 'GLOBAL' : 'ORG'}</span>` : ''}
                    </div>
                </div>
            </div>

            <!-- ── Basic Info ── -->
            <div class="section-label" style="margin-bottom:12px;">Device Information</div>
            <div class="grid grid-2" style="gap:14px;margin-bottom:24px;">
                <div class="form-group">
                    <label>Template Name <span style="color:var(--error)">*</span></label>
                    <input type="text" id="tmpl-name" value="${this._esc(s.name)}" placeholder="e.g. Schneider PM5100">
                </div>
                <div class="form-group">
                    <label>Protocol</label>
                    <input type="text" value="${proto.label}" disabled style="opacity:0.55;">
                </div>
                <div class="form-group">
                    <label>Manufacturer</label>
                    <input type="text" id="tmpl-manufacturer" value="${this._esc(s.manufacturer)}" placeholder="e.g. Schneider Electric">
                </div>
                <div class="form-group">
                    <label>Model</label>
                    <input type="text" id="tmpl-model" value="${this._esc(s.model)}" placeholder="e.g. PM5100">
                </div>
                <div class="form-group" style="grid-column:1/-1;">
                    <label>Description</label>
                    <input type="text" id="tmpl-description" value="${this._esc(s.description)}" placeholder="Short description of what this device measures">
                </div>
            </div>

            ${s.readerTemplate ? `
            <!-- ── Reader Connection Reference ── -->
            <details style="margin-bottom:20px;border:1px solid var(--border);border-radius:10px;padding:12px 16px;">
                <summary style="cursor:pointer;font-size:12px;font-weight:600;color:var(--text-dim);list-style:none;">
                    ℹ️ Reader-level config reference — <code>${s.readerTemplate.name}</code> (set when adding a reader to a Qube, not per device)
                </summary>
                <div style="margin-top:10px;">
                    ${Object.entries(s.readerTemplate.connection_schema?.properties || {}).map(([k, v]) => `
                        <div style="display:flex;gap:12px;padding:4px 0;border-bottom:1px solid rgba(255,255,255,0.04);font-size:11px;">
                            <code style="color:var(--primary);min-width:140px;">${k}</code>
                            <span class="page-subtitle">${v.title || k}</span>
                            <span style="color:var(--text-dim);margin-left:auto;">${v.type}${v.default !== undefined ? ' · default: '+v.default : ''}</span>
                        </div>
                    `).join('')}
                </div>
            </details>` : ''}

            <!-- ── Per-Device Params Schema ── -->
            <div class="flex-between" style="margin-bottom:8px;">
                <div>
                    <div class="section-label">Per-Device Parameters</div>
                    <p class="page-subtitle" style="font-size:11px;margin-top:2px;">Fields filled in when a sensor using this template is added to a reader (e.g. device IP, unit ID)</p>
                </div>
                <div style="display:flex;gap:8px;">
                    <button class="btn btn-ghost btn-sm" id="btn-add-param">+ Field</button>
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-params-json">JSON</button>
                </div>
            </div>
            <div id="params-ui" style="margin-bottom:24px;">
                <div id="params-fields-list"></div>
            </div>
            <div id="params-json-ui" class="hidden" style="margin-bottom:24px;">
                <textarea id="params-json-area" style="height:180px;font-family:'JetBrains Mono',monospace;font-size:11px;background:#1a1d32;width:100%;box-sizing:border-box;"></textarea>
                <div id="params-json-err" class="badge badge-error hidden" style="margin-top:6px;">Invalid JSON</div>
            </div>

            <!-- ── Measurements ── -->
            <div class="flex-between" style="margin-bottom:10px;">
                <div>
                    <div class="section-label">Measurements <span style="font-weight:400;color:var(--text-dim);font-size:11px;">(${arrayKey})</span></div>
                    <p class="page-subtitle" style="font-size:11px;margin-top:2px;">Protocol-specific data points this device exposes</p>
                </div>
                <div style="display:flex;gap:8px;">
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-meas-json">JSON View</button>
                    <button class="btn btn-primary btn-sm" id="btn-add-row">+ Add Row</button>
                </div>
            </div>
            <div id="meas-table-wrap"></div>
            <div id="meas-json-wrap" class="hidden">
                <textarea id="meas-json-area" style="height:300px;font-family:'JetBrains Mono',monospace;font-size:11px;background:#1a1d32;width:100%;box-sizing:border-box;"></textarea>
                <div id="meas-json-err" class="badge badge-error hidden" style="margin-top:6px;">Invalid JSON — must be an array</div>
                <button class="btn btn-ghost btn-sm" id="btn-apply-meas-json" style="margin-top:8px;">Apply Changes</button>
            </div>

            <!-- ── Footer ── -->
            <div class="flex-between" style="margin-top:32px;padding-top:18px;border-top:1px solid var(--border);">
                <button class="btn btn-ghost" id="btn-cancel">Cancel</button>
                <button class="btn btn-primary" id="btn-save">${isEdit ? 'Save Changes' : 'Create Template'}</button>
            </div>
        `;

        if (!isEdit) document.getElementById('btn-back-proto').onclick = () => this._renderProtocolStep();

        // Params
        this._renderParamsFields();

        document.getElementById('btn-add-param').onclick = () => {
            const props = s.sensorParamsSchema.properties || {};
            const newKey = `param${Object.keys(props).length + 1}`;
            props[newKey] = { type: 'string', title: '' };
            s.sensorParamsSchema.properties = props;
            this._renderParamsFields();
        };

        document.getElementById('btn-toggle-params-json').onclick = () => {
            const ui = document.getElementById('params-ui');
            const je = document.getElementById('params-json-ui');
            const wasJson = !je.classList.contains('hidden');
            if (wasJson) {
                try {
                    s.sensorParamsSchema = JSON.parse(document.getElementById('params-json-area').value);
                    document.getElementById('params-json-err').classList.add('hidden');
                    this._renderParamsFields();
                } catch (e) {
                    document.getElementById('params-json-err').classList.remove('hidden');
                    return;
                }
            } else {
                document.getElementById('params-json-area').value = JSON.stringify(s.sensorParamsSchema, null, 2);
            }
            ui.classList.toggle('hidden');
            je.classList.toggle('hidden');
        };

        // Measurements
        this._renderMeasTable(fields, arrayKey);

        document.getElementById('btn-add-row').onclick = () => {
            const entry = {};
            fields.forEach(f => { entry[f.key] = f.default !== undefined ? f.default : ''; });
            s.measurements.push(entry);
            this._renderMeasTable(fields, arrayKey);
        };

        document.getElementById('btn-toggle-meas-json').onclick = () => {
            const tbl = document.getElementById('meas-table-wrap');
            const je = document.getElementById('meas-json-wrap');
            const wasJson = !je.classList.contains('hidden');
            if (wasJson) {
                try {
                    const parsed = JSON.parse(document.getElementById('meas-json-area').value);
                    if (!Array.isArray(parsed)) throw new Error('not array');
                    s.measurements = parsed;
                    document.getElementById('meas-json-err').classList.add('hidden');
                    this._renderMeasTable(fields, arrayKey);
                } catch (e) {
                    document.getElementById('meas-json-err').classList.remove('hidden');
                    return;
                }
            } else {
                document.getElementById('meas-json-area').value = JSON.stringify(s.measurements, null, 2);
            }
            tbl.classList.toggle('hidden');
            je.classList.toggle('hidden');
        };

        document.getElementById('btn-apply-meas-json').onclick = () => {
            try {
                const parsed = JSON.parse(document.getElementById('meas-json-area').value);
                if (!Array.isArray(parsed)) throw new Error('not array');
                s.measurements = parsed;
                document.getElementById('meas-json-err').classList.add('hidden');
                document.getElementById('meas-table-wrap').classList.remove('hidden');
                document.getElementById('meas-json-wrap').classList.add('hidden');
                this._renderMeasTable(fields, arrayKey);
            } catch (e) {
                document.getElementById('meas-json-err').classList.remove('hidden');
            }
        };

        document.getElementById('btn-cancel').onclick = () => this._closeModal();
        document.getElementById('btn-save').onclick = () => this._save(fields, arrayKey);
    },

    // ── Params Schema UI ───────────────────────────────────────────────────────

    _renderParamsFields() {
        const s = this._editState;
        const container = document.getElementById('params-fields-list');
        if (!container) return;

        const props = s.sensorParamsSchema?.properties || {};
        const required = s.sensorParamsSchema?.required || [];
        const entries = Object.entries(props);

        if (entries.length === 0) {
            container.innerHTML = '<div class="page-subtitle" style="font-size:12px;padding:6px 0 10px;">No parameters defined.</div>';
            return;
        }

        container.innerHTML = '';

        // Header row
        const hdr = document.createElement('div');
        hdr.className = 'param-row';
        hdr.innerHTML = `
            <div style="font-size:11px;color:var(--text-dim);font-weight:600;">Field Key</div>
            <div style="font-size:11px;color:var(--text-dim);font-weight:600;">Title (display label)</div>
            <div style="font-size:11px;color:var(--text-dim);font-weight:600;">Type</div>
            <div style="font-size:11px;color:var(--text-dim);font-weight:600;">Required</div>
            <div></div>
        `;
        container.appendChild(hdr);

        entries.forEach(([key, prop], idx) => {
            const row = document.createElement('div');
            row.className = 'param-row';
            row.dataset.origKey = key;

            const selType = `<select class="meas-input param-type" style="min-width:unset;">
                <option value="string" ${prop.type === 'string' ? 'selected' : ''}>string</option>
                <option value="integer" ${prop.type === 'integer' ? 'selected' : ''}>integer</option>
                <option value="number" ${prop.type === 'number' ? 'selected' : ''}>number</option>
            </select>`;
            const selReq = `<select class="meas-input param-req" style="min-width:unset;">
                <option value="no" ${!required.includes(key) ? 'selected' : ''}>No</option>
                <option value="yes" ${required.includes(key) ? 'selected' : ''}>Yes</option>
            </select>`;

            row.innerHTML = `
                <input class="meas-input param-key" type="text" value="${this._esc(key)}" placeholder="field_key">
                <input class="meas-input param-title" type="text" value="${this._esc(prop.title || '')}" placeholder="Human readable label">
                ${selType}
                ${selReq}
                <button class="btn btn-ghost btn-sm param-del" style="padding:4px 8px;">✕</button>
            `;
            container.appendChild(row);
        });

        // Sync on any change
        const syncParams = () => {
            const newProps = {};
            const newRequired = [];
            container.querySelectorAll('.param-row[data-orig-key]').forEach(row => {
                const k = row.querySelector('.param-key').value.trim();
                if (!k) return;
                const t = row.querySelector('.param-type').value;
                const title = row.querySelector('.param-title').value;
                const isReq = row.querySelector('.param-req').value === 'yes';
                newProps[k] = { type: t, title };
                if (isReq) newRequired.push(k);
                row.dataset.origKey = k;
            });
            s.sensorParamsSchema = { type: 'object', properties: newProps, required: newRequired };
        };

        container.querySelectorAll('input, select').forEach(el => el.addEventListener('change', syncParams));
        container.querySelectorAll('input').forEach(el => el.addEventListener('input', syncParams));

        container.querySelectorAll('.param-del').forEach((btn, i) => {
            btn.onclick = () => {
                const row = btn.closest('.param-row[data-orig-key]');
                const k = row.dataset.origKey;
                delete s.sensorParamsSchema.properties[k];
                s.sensorParamsSchema.required = (s.sensorParamsSchema.required || []).filter(r => r !== k);
                this._renderParamsFields();
            };
        });
    },

    // ── Measurements Table ─────────────────────────────────────────────────────

    _renderMeasTable(fields, arrayKey) {
        const s = this._editState;
        const container = document.getElementById('meas-table-wrap');
        if (!container) return;

        if (s.measurements.length === 0) {
            container.innerHTML = '<div class="page-subtitle" style="font-size:12px;padding:10px 0;">No measurements defined yet. Click "+ Add Row" to add one.</div>';
            return;
        }

        const wrap = document.createElement('div');
        wrap.className = 'table-container';
        wrap.style.overflowX = 'auto';
        const table = document.createElement('table');
        table.style.minWidth = '500px';

        const thead = document.createElement('thead');
        thead.innerHTML = `<tr>${fields.map(f => `<th style="white-space:nowrap;">${f.label}</th>`).join('')}<th style="width:36px;"></th></tr>`;
        table.appendChild(thead);

        const tbody = document.createElement('tbody');
        s.measurements.forEach((entry, rowIdx) => {
            const tr = document.createElement('tr');

            fields.forEach(f => {
                const td = document.createElement('td');
                td.style.padding = '5px 6px';

                if (f.type === 'select') {
                    const sel = document.createElement('select');
                    sel.className = 'meas-input';
                    sel.style.minWidth = 'unset';
                    (f.options || []).forEach(opt => {
                        const o = document.createElement('option');
                        o.value = opt;
                        o.textContent = opt;
                        const cur = entry[f.key] !== undefined ? entry[f.key] : f.default;
                        if (cur === opt) o.selected = true;
                        sel.appendChild(o);
                    });
                    sel.onchange = () => { s.measurements[rowIdx][f.key] = sel.value; };
                    td.appendChild(sel);
                } else {
                    const inp = document.createElement('input');
                    inp.className = 'meas-input';
                    inp.type = f.type === 'number' ? 'number' : 'text';
                    inp.placeholder = f.placeholder || '';
                    inp.value = entry[f.key] !== undefined ? entry[f.key] : (f.default !== undefined ? f.default : '');
                    inp.oninput = () => {
                        s.measurements[rowIdx][f.key] = f.type === 'number' ? (parseFloat(inp.value) || 0) : inp.value;
                    };
                    td.appendChild(inp);
                }
                tr.appendChild(td);
            });

            const delTd = document.createElement('td');
            delTd.style.padding = '5px 4px';
            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-ghost btn-sm';
            delBtn.textContent = '✕';
            delBtn.style.padding = '4px 8px';
            delBtn.onclick = () => {
                s.measurements.splice(rowIdx, 1);
                this._renderMeasTable(fields, arrayKey);
            };
            delTd.appendChild(delBtn);
            tr.appendChild(delTd);
            tbody.appendChild(tr);
        });

        table.appendChild(tbody);
        wrap.appendChild(table);
        container.innerHTML = '';
        container.appendChild(wrap);
    },

    // ── Save ───────────────────────────────────────────────────────────────────

    async _save(fields, arrayKey) {
        const s = this._editState;
        const name = document.getElementById('tmpl-name').value.trim();
        if (!name) { Components.showAlert('Template name is required', 'error'); return; }

        const manufacturer = document.getElementById('tmpl-manufacturer').value.trim();
        const model        = document.getElementById('tmpl-model').value.trim();
        const description  = document.getElementById('tmpl-description').value.trim();

        const payload = {
            name, manufacturer, model, description,
            sensor_config: { [arrayKey]: s.measurements },
            sensor_params_schema: s.sensorParamsSchema,
        };

        const btn = document.getElementById('btn-save');
        btn.disabled = true;
        btn.textContent = 'Saving...';

        try {
            if (s.mode === 'edit') {
                await API.put(`/api/v1/device-templates/${s.id}`, payload);
                Components.showAlert('Template updated');
            } else {
                payload.protocol = s.protocol.id;
                await API.post('/api/v1/device-templates', payload);
                Components.showAlert('Template created');
            }
            this._closeModal();
            const filter = document.getElementById('template-proto-filter');
            await this.loadTemplates(filter ? filter.value : '');
        } catch (err) {
            Components.showAlert(err.message, 'error');
            if (btn) { btn.disabled = false; btn.textContent = s.mode === 'edit' ? 'Save Changes' : 'Create Template'; }
        }
    },

    // ── Edit ───────────────────────────────────────────────────────────────────

    async _showEditModal(id) {
        try {
            const t = await API.getDeviceTemplate(id);
            const proto = this._protocols.find(p => p.id === t.protocol) || { id: t.protocol, label: t.protocol, sensor_config_key: 'entries', measurement_fields_schema: [], icon: '🔧' };
            const arrayKey = proto.sensor_config_key || 'entries';
            const measurements = JSON.parse(JSON.stringify(t.sensor_config?.[arrayKey] || []));

            let readerTemplate = null;
            try {
                const rts = await API.getReaderTemplates(t.protocol);
                readerTemplate = rts[0] || null;
            } catch (e) { /* ignore */ }

            this._editState = {
                mode: 'edit',
                id: t.id,
                is_global: t.is_global,
                protocol: proto,
                readerTemplate,
                name: t.name || '',
                manufacturer: t.manufacturer || '',
                model: t.model || '',
                description: t.description || '',
                measurements,
                sensorParamsSchema: JSON.parse(JSON.stringify(
                    t.sensor_params_schema && Object.keys(t.sensor_params_schema).length
                        ? t.sensor_params_schema
                        : { type: 'object', properties: {}, required: [] }
                )),
            };
            this._openModal();
            this._renderMainForm();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    // ── Helpers ────────────────────────────────────────────────────────────────

    _esc(str) {
        if (str === null || str === undefined) return '';
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/"/g, '&quot;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    },
};

export default Templates;
