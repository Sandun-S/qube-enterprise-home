import API from '../api.js';
import Components from '../components.js';

const Sensors = {
    _allSensors: [],

    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Enterprise Sensors</h2>
                    <p class="page-subtitle">Combined view of all physical devices in the organization</p>
                </div>
            </div>

            <div class="card">
                <div class="card-title">All Registered Sensors</div>
                <div id="sensors-table-container">
                    <div class="text-center page-subtitle">Fetching sensor registry...</div>
                </div>
            </div>

            <!-- Edit Sensor Modal -->
            <div id="edit-sensor-modal" class="modal-backdrop hidden">
                <div class="modal" style="max-width: 640px;">
                    <div class="modal-header">
                        <h2 style="font-size: 18px;">Edit Sensor</h2>
                        <p id="edit-sensor-subtitle" class="page-subtitle" style="font-size: 12px; margin-top: 4px;"></p>
                    </div>
                    <div class="modal-body">
                        <div class="grid grid-2">
                            <div class="form-group">
                                <label>Sensor Name</label>
                                <input type="text" id="edit-sensor-name" placeholder="Sensor name">
                            </div>
                            <div class="form-group">
                                <label>Output Mode</label>
                                <select id="edit-sensor-output">
                                    <option value="influxdb">influxdb</option>
                                    <option value="live">live</option>
                                    <option value="influxdb,live">influxdb,live</option>
                                </select>
                            </div>
                        </div>
                        <div class="grid grid-2">
                            <div class="form-group">
                                <label>Table Name</label>
                                <input type="text" id="edit-sensor-table" placeholder="Measurements">
                            </div>
                            <div class="form-group">
                                <label>Status</label>
                                <select id="edit-sensor-status">
                                    <option value="active">active</option>
                                    <option value="disabled">disabled</option>
                                </select>
                            </div>
                        </div>
                        <div class="form-group">
                            <label>Tags (JSON)</label>
                            <input type="text" id="edit-sensor-tags" placeholder='{"location": "Room A"}'>
                        </div>
                        <div class="form-group">
                            <div class="flex-between" style="margin-bottom: 8px;">
                                <label style="margin: 0;">Sensor Config (JSON)</label>
                                <button id="btn-toggle-config-editor" class="btn btn-ghost btn-sm">🛠️ Edit Raw Config</button>
                            </div>
                            <div id="config-editor-hint" class="page-subtitle" style="font-size: 11px; margin-bottom: 8px;">
                                Toggle to edit OIDs / registers / json_paths / node_ids directly.
                            </div>
                            <textarea id="edit-sensor-config" style="display:none; height: 260px; font-family: 'JetBrains Mono', monospace; font-size: 11px; background: #1a1d32;" placeholder="{}"></textarea>
                            <div id="edit-config-error" class="badge badge-error hidden" style="margin-top: 6px;">Invalid JSON</div>
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-edit-sensor-cancel" class="btn btn-ghost">Cancel</button>
                        <button id="btn-edit-sensor-save" class="btn btn-primary">Save Changes</button>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this.loadSensors();
        this._bindModal();
    },

    _bindModal() {
        document.getElementById('btn-edit-sensor-cancel')?.addEventListener('click', () => {
            document.getElementById('edit-sensor-modal').classList.add('hidden');
        });

        document.getElementById('btn-toggle-config-editor')?.addEventListener('click', () => {
            const ta = document.getElementById('edit-sensor-config');
            const visible = ta.style.display !== 'none';
            ta.style.display = visible ? 'none' : 'block';
            document.getElementById('btn-toggle-config-editor').textContent = visible ? '🛠️ Edit Raw Config' : '✕ Hide Config';
        });

        document.getElementById('btn-edit-sensor-save')?.addEventListener('click', () => this._saveSensor());
    },

    async loadSensors() {
        try {
            const qubes = await API.getQubes();
            let allSensors = [];

            for (const qube of qubes) {
                const readers = await API.getQubeReaders(qube.id);
                for (const r of readers) {
                    const sensors = await API.getReaderSensors(r.id);
                    allSensors = allSensors.concat(sensors.map(s => ({
                        ...s, readerName: r.name, readerId: r.id, protocol: r.protocol, qubeId: qube.id
                    })));
                }
            }

            this._allSensors = allSensors;

            const headerActions = `<div class="flex-between mb-20" style="padding: 0 16px;">
                <button class="btn btn-ghost btn-sm" id="btn-toggle-sensors-raw">👁️ View Raw JSON</button>
            </div>
            <pre id="sensors-raw-json" class="raw-json-preview hidden" style="margin: 16px;">${JSON.stringify(allSensors, null, 2)}</pre>`;

            document.getElementById('sensors-table-container').innerHTML = headerActions + '<div id="sensors-table"></div>';

            document.getElementById('btn-toggle-sensors-raw').onclick = () => {
                document.getElementById('sensors-raw-json').classList.toggle('hidden');
            };

            Components.renderTable(
                ['Sensor Name', 'Qube', 'Reader', 'Protocol', 'Output', 'Status', 'Actions'],
                allSensors,
                'sensors-table',
                (s) => [
                    `<b>${s.name}</b><div style="font-size: 10px; color: var(--text-dim);">${s.template_name || 'custom'}</div>`,
                    `<code>${s.qubeId}</code>`,
                    `<span style="font-size: 12px;">${s.readerName}</span>`,
                    `<span class="badge badge-blue" style="font-size: 9px;">${s.protocol || ''}</span>`,
                    `<span class="badge" style="font-size: 9px; background: rgba(255,255,255,0.05);">${s.output}</span>`,
                    `<span class="badge badge-${s.status === 'active' ? 'success' : 'error'}">${s.status}</span>`,
                    `<div class="flex">
                        <button class="btn btn-primary btn-sm btn-sensor-live" data-id="${s.id}">Live</button>
                        <button class="btn btn-ghost btn-sm btn-sensor-edit" data-id="${s.id}">Edit</button>
                        <button class="btn btn-ghost btn-sm btn-sensor-delete" data-id="${s.id}" data-name="${s.name}" style="color: var(--error);">Del</button>
                    </div>`
                ]
            );

            document.querySelectorAll('.btn-sensor-live').forEach(btn => {
                btn.onclick = () => { window.location.hash = `#telemetry?sensor_id=${btn.dataset.id}`; };
            });
            document.querySelectorAll('.btn-sensor-edit').forEach(btn => {
                btn.onclick = () => this._openEdit(btn.dataset.id);
            });
            document.querySelectorAll('.btn-sensor-delete').forEach(btn => {
                btn.onclick = () => this._deleteSensor(btn.dataset.id, btn.dataset.name);
            });

        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    _openEdit(sensorId) {
        const s = this._allSensors.find(x => x.id === sensorId);
        if (!s) return;

        this._editingId = sensorId;
        document.getElementById('edit-sensor-subtitle').textContent = `ID: ${s.id} | Reader: ${s.readerName}`;
        document.getElementById('edit-sensor-name').value = s.name;
        document.getElementById('edit-sensor-output').value = s.output || 'influxdb';
        document.getElementById('edit-sensor-table').value = s.table_name || 'Measurements';
        document.getElementById('edit-sensor-status').value = s.status || 'active';
        document.getElementById('edit-sensor-tags').value = s.tags_json ? JSON.stringify(s.tags_json) : '{}';
        document.getElementById('edit-sensor-config').value = s.config_json ? JSON.stringify(s.config_json, null, 2) : '{}';
        document.getElementById('edit-config-error').classList.add('hidden');
        // Reset config editor visibility
        document.getElementById('edit-sensor-config').style.display = 'none';
        document.getElementById('btn-toggle-config-editor').textContent = '🛠️ Edit Raw Config';

        document.getElementById('edit-sensor-modal').classList.remove('hidden');
    },

    async _saveSensor() {
        const btn = document.getElementById('btn-edit-sensor-save');
        btn.disabled = true;
        btn.textContent = 'Saving...';

        try {
            const name = document.getElementById('edit-sensor-name').value.trim();
            const output = document.getElementById('edit-sensor-output').value;
            const table_name = document.getElementById('edit-sensor-table').value.trim();
            const status = document.getElementById('edit-sensor-status').value;
            const tagsRaw = document.getElementById('edit-sensor-tags').value.trim();
            const configRaw = document.getElementById('edit-sensor-config').value.trim();

            const payload = { name, output, table_name, status };

            try {
                payload.tags_json = tagsRaw ? JSON.parse(tagsRaw) : {};
            } catch {
                Components.showAlert('Tags JSON is invalid', 'error');
                return;
            }

            // Only send config if the editor was used (textarea is visible or has been changed)
            const configEditor = document.getElementById('edit-sensor-config');
            if (configEditor.style.display !== 'none' && configRaw) {
                try {
                    payload.config_json = JSON.parse(configRaw);
                } catch {
                    document.getElementById('edit-config-error').classList.remove('hidden');
                    return;
                }
            }

            await API.updateSensor(this._editingId, payload);
            document.getElementById('edit-sensor-modal').classList.add('hidden');
            Components.showAlert('Sensor updated. Edge sync triggered.', 'success');
            this.loadSensors();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        } finally {
            btn.disabled = false;
            btn.textContent = 'Save Changes';
        }
    },

    async _deleteSensor(sensorId, sensorName) {
        if (!confirm(`Delete sensor "${sensorName}"? This will trigger a config sync to the edge device.`)) return;
        try {
            await API.deleteSensor(sensorId);
            Components.showAlert('Sensor deleted. Edge sync triggered.', 'success');
            this.loadSensors();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

export default Sensors;
