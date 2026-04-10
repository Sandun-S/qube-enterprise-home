import API from '../api.js';
import Components from '../components.js';

/**
 * Telemetry Mappings page
 *
 * Manages the telemetry_settings table: each row tells enterprise-influx-to-sql
 * which InfluxDB device+reading maps to which cloud sensor UUID.
 *
 * Flow:
 *   InfluxDB measurement (device tag + field name)
 *     → telemetry_settings row (device + reading → sensor_id)
 *     → conf-agent syncs to Qube SQLite
 *     → enterprise-influx-to-sql reads SQLite, maps & POSTs to TP-API
 *     → TimescaleDB qubedata.sensor_readings
 */

const TelemetrySettings = {
    _qubes: [],
    _sensors: [],      // sensors for the selected Qube
    _mappings: [],     // current telemetry_settings rows
    _selectedQube: '',
    _editingId: null,  // ts_id being edited, null = create mode

    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Telemetry Mappings</h2>
                    <p class="page-subtitle">
                        Map InfluxDB device+reading fields to cloud sensor IDs so
                        <code>enterprise-influx-to-sql</code> can forward edge data to TimescaleDB.
                    </p>
                </div>
                <button id="btn-new-mapping" class="btn btn-primary" disabled>+ Add Mapping</button>
            </div>

            <!-- Qube selector -->
            <div class="card" style="margin-bottom:16px;">
                <div class="card-title">Select Qube</div>
                <div style="display:flex;gap:12px;align-items:center;">
                    <select id="ts-qube-select" style="max-width:320px;">
                        <option value="">Loading qubes...</option>
                    </select>
                    <span id="ts-qube-status" class="page-subtitle" style="font-size:12px;"></span>
                </div>
            </div>

            <!-- How it works callout -->
            <div id="ts-howto" class="card" style="margin-bottom:16px;border-left:3px solid var(--primary);display:none;">
                <div style="font-size:12px;color:var(--text-dim);line-height:1.7;">
                    <strong style="color:var(--text);">How it works:</strong>
                    Each mapping tells <code>enterprise-influx-to-sql</code> that the InfluxDB
                    measurement matching <strong>Device</strong> (equipment tag) +
                    <strong>Reading</strong> (field key) should be stored under
                    <strong>Sensor ID</strong> in TimescaleDB.<br>
                    Use <code>*</code> for Reading to match <em>all</em> fields from that device.
                    Changes sync to Qube SQLite on the next config pull (config hash changes immediately).
                </div>
            </div>

            <!-- Mappings table -->
            <div class="card">
                <div class="flex-between" style="margin-bottom:14px;">
                    <div class="card-title" style="margin:0;">Mappings</div>
                    <span id="ts-count" class="badge badge-blue" style="display:none;"></span>
                </div>
                <div id="ts-table-container">
                    <div class="text-center page-subtitle">Select a Qube above to load mappings.</div>
                </div>
            </div>

            <!-- Add / Edit Modal -->
            <div id="ts-modal" class="modal-backdrop hidden">
                <div class="modal" style="max-width:520px;">
                    <div class="modal-header">
                        <h2 id="ts-modal-title" style="font-size:18px;">Add Telemetry Mapping</h2>
                        <p class="page-subtitle" style="font-size:12px;margin-top:4px;">
                            Link an InfluxDB device field to a cloud sensor UUID.
                        </p>
                    </div>
                    <div class="modal-body">
                        <div class="grid grid-2">
                            <div class="form-group">
                                <label>Device <span style="color:var(--error)">*</span>
                                    <span style="font-weight:400;color:var(--text-dim);font-size:11px;">
                                        (InfluxDB equipment tag)
                                    </span>
                                </label>
                                <input type="text" id="ts-device" placeholder="e.g. PM3000_Phase_A">
                            </div>
                            <div class="form-group">
                                <label>Reading
                                    <span style="font-weight:400;color:var(--text-dim);font-size:11px;">
                                        (field key, * = all)
                                    </span>
                                </label>
                                <input type="text" id="ts-reading" placeholder="* or voltage">
                            </div>
                        </div>
                        <div class="form-group">
                            <label>Sensor <span style="color:var(--error)">*</span></label>
                            <select id="ts-sensor-select">
                                <option value="">Select sensor...</option>
                            </select>
                            <div class="page-subtitle" style="font-size:11px;margin-top:4px;">
                                Only sensors that belong to this Qube are listed.
                            </div>
                        </div>
                        <div class="grid grid-2">
                            <div class="form-group">
                                <label>Aggregation Function</label>
                                <select id="ts-agg-func">
                                    <option value="LAST">LAST (default — latest value)</option>
                                    <option value="AVG">AVG</option>
                                    <option value="MAX">MAX</option>
                                    <option value="MIN">MIN</option>
                                    <option value="SUM">SUM</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label>Aggregation Window (minutes)</label>
                                <input type="number" id="ts-agg-time" value="1" min="1">
                            </div>
                        </div>
                        <div id="ts-modal-error" class="badge badge-error hidden" style="margin-top:8px;"></div>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-ts-cancel" class="btn btn-ghost">Cancel</button>
                        <button id="btn-ts-save" class="btn btn-primary">Save Mapping</button>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        await this._loadQubes();
        this._bindEvents();
    },

    async _loadQubes() {
        const sel = document.getElementById('ts-qube-select');
        try {
            this._qubes = await API.getQubes();
            sel.innerHTML = '<option value="">— select a qube —</option>' +
                this._qubes.map(q =>
                    `<option value="${q.id}">${q.id}${q.location_label ? ' · ' + q.location_label : ''} (${q.status || 'unknown'})</option>`
                ).join('');
        } catch (e) {
            sel.innerHTML = '<option value="">Failed to load qubes</option>';
        }
    },

    async _loadMappings(qubeId) {
        this._selectedQube = qubeId;
        const container = document.getElementById('ts-table-container');
        const countBadge = document.getElementById('ts-count');
        const newBtn = document.getElementById('btn-new-mapping');
        const howto = document.getElementById('ts-howto');

        if (!qubeId) {
            container.innerHTML = '<div class="text-center page-subtitle">Select a Qube above to load mappings.</div>';
            countBadge.style.display = 'none';
            newBtn.disabled = true;
            howto.style.display = 'none';
            return;
        }

        container.innerHTML = '<div class="text-center page-subtitle">Loading...</div>';
        newBtn.disabled = true;
        howto.style.display = '';

        try {
            const [mappings, sensors] = await Promise.all([
                API.getTelemetrySettings(qubeId),
                API.request('GET', `/api/v1/qubes/${qubeId}/sensors`),
            ]);
            this._mappings = mappings;
            this._sensors = sensors;

            newBtn.disabled = false;
            countBadge.textContent = `${mappings.length} mapping${mappings.length !== 1 ? 's' : ''}`;
            countBadge.style.display = '';

            if (mappings.length === 0) {
                container.innerHTML = `
                    <div class="text-center page-subtitle" style="padding:32px 0;">
                        No mappings yet for this Qube.<br>
                        <strong>enterprise-influx-to-sql will skip all data</strong> until at least one mapping exists.<br>
                        Click <em>+ Add Mapping</em> to create one.
                    </div>`;
                return;
            }

            container.innerHTML = `
                <table style="width:100%;border-collapse:collapse;font-size:13px;">
                    <thead>
                        <tr style="border-bottom:1px solid var(--border);color:var(--text-dim);text-align:left;">
                            <th style="padding:8px 12px;">Device (InfluxDB tag)</th>
                            <th style="padding:8px 12px;">Reading (field)</th>
                            <th style="padding:8px 12px;">Sensor</th>
                            <th style="padding:8px 12px;">Agg</th>
                            <th style="padding:8px 12px;text-align:right;">Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${mappings.map(m => `
                            <tr style="border-bottom:1px solid var(--border);" data-ts-id="${m.id}">
                                <td style="padding:10px 12px;font-family:'JetBrains Mono',monospace;font-size:12px;">${m.device}</td>
                                <td style="padding:10px 12px;font-family:'JetBrains Mono',monospace;font-size:12px;">
                                    ${m.reading === '*'
                                        ? '<span class="badge badge-blue" style="font-size:11px;">* (all fields)</span>'
                                        : m.reading}
                                </td>
                                <td style="padding:10px 12px;">
                                    <div style="font-size:12px;font-weight:600;">${m.sensor_name || '—'}</div>
                                    <div style="font-size:11px;color:var(--text-dim);font-family:'JetBrains Mono',monospace;">${m.sensor_id || '—'}</div>
                                </td>
                                <td style="padding:10px 12px;font-size:12px;color:var(--text-dim);">
                                    ${m.agg_func} / ${m.agg_time_min}m
                                </td>
                                <td style="padding:10px 12px;text-align:right;">
                                    <button class="btn btn-ghost btn-sm btn-edit-ts" data-id="${m.id}" style="margin-right:6px;">Edit</button>
                                    <button class="btn btn-ghost btn-sm btn-delete-ts" data-id="${m.id}" style="color:var(--error);">Delete</button>
                                </td>
                            </tr>`).join('')}
                    </tbody>
                </table>`;

            // Bind row actions
            container.querySelectorAll('.btn-edit-ts').forEach(btn =>
                btn.addEventListener('click', () => this._openModal(btn.dataset.id)));
            container.querySelectorAll('.btn-delete-ts').forEach(btn =>
                btn.addEventListener('click', () => this._deleteMapping(btn.dataset.id)));

        } catch (e) {
            container.innerHTML = `<div class="badge badge-error">${e.message}</div>`;
            newBtn.disabled = false;
        }
    },

    _bindEvents() {
        document.getElementById('ts-qube-select').addEventListener('change', e => {
            this._loadMappings(e.target.value);
        });
        document.getElementById('btn-new-mapping').addEventListener('click', () => this._openModal(null));
        document.getElementById('btn-ts-cancel').addEventListener('click', () => this._closeModal());
        document.getElementById('btn-ts-save').addEventListener('click', () => this._saveMapping());
    },

    _openModal(tsId) {
        this._editingId = tsId || null;
        const modal = document.getElementById('ts-modal');
        const title = document.getElementById('ts-modal-title');
        const errEl = document.getElementById('ts-modal-error');

        errEl.classList.add('hidden');
        title.textContent = tsId ? 'Edit Telemetry Mapping' : 'Add Telemetry Mapping';

        // Populate sensor dropdown
        const sel = document.getElementById('ts-sensor-select');
        sel.innerHTML = '<option value="">Select sensor...</option>' +
            this._sensors.map(s =>
                `<option value="${s.id}">${s.name} (${s.protocol || ''})</option>`
            ).join('');

        if (tsId) {
            // Pre-fill from existing mapping
            const m = this._mappings.find(x => x.id === tsId);
            if (m) {
                document.getElementById('ts-device').value = m.device || '';
                document.getElementById('ts-reading').value = m.reading || '*';
                document.getElementById('ts-agg-func').value = m.agg_func || 'LAST';
                document.getElementById('ts-agg-time').value = m.agg_time_min || 1;
                sel.value = m.sensor_id || '';
            }
        } else {
            document.getElementById('ts-device').value = '';
            document.getElementById('ts-reading').value = '*';
            document.getElementById('ts-agg-func').value = 'LAST';
            document.getElementById('ts-agg-time').value = '1';
            sel.value = '';
        }

        modal.classList.remove('hidden');
        document.getElementById('ts-device').focus();
    },

    _closeModal() {
        document.getElementById('ts-modal').classList.add('hidden');
        this._editingId = null;
    },

    async _saveMapping() {
        const device = document.getElementById('ts-device').value.trim();
        const reading = document.getElementById('ts-reading').value.trim() || '*';
        const sensorId = document.getElementById('ts-sensor-select').value;
        const aggFunc = document.getElementById('ts-agg-func').value;
        const aggTime = parseInt(document.getElementById('ts-agg-time').value, 10) || 1;
        const errEl = document.getElementById('ts-modal-error');

        if (!device) {
            errEl.textContent = 'Device is required.';
            errEl.classList.remove('hidden');
            return;
        }
        if (!sensorId) {
            errEl.textContent = 'Please select a sensor.';
            errEl.classList.remove('hidden');
            return;
        }

        errEl.classList.add('hidden');
        const saveBtn = document.getElementById('btn-ts-save');
        saveBtn.disabled = true;
        saveBtn.textContent = 'Saving...';

        try {
            const payload = { device, reading, sensor_id: sensorId, agg_func: aggFunc, agg_time_min: aggTime };

            if (this._editingId) {
                await API.updateTelemetrySetting(this._selectedQube, this._editingId, payload);
                Components.showAlert('Mapping updated. Config hash changed — Qube will re-sync.', 'blue');
            } else {
                await API.createTelemetrySetting(this._selectedQube, payload);
                Components.showAlert('Mapping created. Config hash changed — Qube will re-sync.', 'blue');
            }

            this._closeModal();
            await this._loadMappings(this._selectedQube);
        } catch (e) {
            errEl.textContent = e.message;
            errEl.classList.remove('hidden');
        } finally {
            saveBtn.disabled = false;
            saveBtn.textContent = 'Save Mapping';
        }
    },

    async _deleteMapping(tsId) {
        if (!confirm('Delete this telemetry mapping? enterprise-influx-to-sql will stop forwarding this device field.')) return;
        try {
            await API.deleteTelemetrySetting(this._selectedQube, tsId);
            Components.showAlert('Mapping deleted.', 'blue');
            await this._loadMappings(this._selectedQube);
        } catch (e) {
            Components.showAlert(e.message, 'error');
        }
    },
};

export default TelemetrySettings;
