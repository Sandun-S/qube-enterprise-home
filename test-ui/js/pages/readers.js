import API from '../api.js';
import Components from '../components.js';

// Extract a meaningful "connection summary" from any reader config regardless of protocol
function getConnectionSummary(r) {
    const c = r.config_json || {};
    if (c.broker_host) {
        const auth = c.username ? `${c.username}@` : '';
        return `${auth}${c.broker_host}:${c.broker_port || 1883}`;
    }
    if (c.host) return `${c.host}:${c.port || ''}`;
    if (c.endpoint) return c.endpoint;
    if (c.ns_host) return `${c.ns_host}:${c.ns_port || ''}`;
    return 'N/A';
}

const Readers = {
    _allReaders: [],

    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Edge Readers</h2>
                    <p class="page-subtitle">Managed protocol containers running on Qubes</p>
                </div>
            </div>

            <div class="card">
                <div class="card-title">All Active Readers</div>
                <div id="readers-table-container">
                    <div class="text-center page-subtitle">Loading reader registry...</div>
                </div>
            </div>

            <!-- Sensors Modal -->
            <div id="sensors-modal" class="modal-backdrop hidden">
                <div class="modal" style="max-width: 900px;">
                    <div class="modal-header">
                        <h2 id="modal-reader-name" style="font-size: 18px;">Sensors</h2>
                        <p id="modal-reader-conn" class="page-subtitle" style="font-size: 12px; margin-top: 4px;"></p>
                    </div>
                    <div class="modal-body">
                        <div id="sensors-table-container"></div>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-modal-close" class="btn btn-ghost">Close</button>
                    </div>
                </div>
            </div>

            <!-- Reader Config Modal -->
            <div id="reader-config-modal" class="modal-backdrop hidden">
                <div class="modal" style="max-width: 560px;">
                    <div class="modal-header">
                        <h2 style="font-size: 18px;">Reader Configuration</h2>
                        <p id="reader-config-name" class="page-subtitle" style="font-size: 12px; margin-top: 4px;"></p>
                    </div>
                    <div class="modal-body">
                        <pre id="reader-config-json" style="font-size: 11px; font-family: 'JetBrains Mono', monospace; background: rgba(0,0,0,0.3); padding: 16px; border-radius: 8px; max-height: 420px; overflow: auto;"></pre>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-reader-config-close" class="btn btn-ghost">Close</button>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this.loadReaders();
        document.getElementById('btn-modal-close')?.addEventListener('click', () => {
            document.getElementById('sensors-modal').classList.add('hidden');
        });
        document.getElementById('btn-reader-config-close')?.addEventListener('click', () => {
            document.getElementById('reader-config-modal').classList.add('hidden');
        });
    },

    async loadReaders() {
        try {
            const qubes = await API.getQubes();
            let allReaders = [];

            for (const qube of qubes) {
                const readers = await API.getQubeReaders(qube.id);
                allReaders = allReaders.concat(readers.map(r => ({ ...r, qubeId: qube.id })));
            }

            this._allReaders = allReaders;

            const headerActions = `<div class="flex-between mb-20" style="padding: 0 16px;">
                <button class="btn btn-ghost btn-sm" id="btn-toggle-readers-raw">👁️ View Raw JSON</button>
            </div>
            <pre id="readers-raw-json" class="raw-json-preview hidden" style="margin: 16px;">${JSON.stringify(allReaders, null, 2)}</pre>`;

            document.getElementById('readers-table-container').innerHTML = headerActions + '<div id="readers-table"></div>';

            document.getElementById('btn-toggle-readers-raw').onclick = () => {
                document.getElementById('readers-raw-json').classList.toggle('hidden');
            };

            Components.renderTable(
                ['Reader Name', 'Protocol', 'Qube', 'Connection', 'Sensors', 'Status', 'Actions'],
                allReaders,
                'readers-table',
                (r) => [
                    `<b>${r.name}</b>`,
                    `<span class="badge badge-blue">${r.protocol}</span>`,
                    `<code>${r.qubeId}</code>`,
                    `<span style="font-family: 'JetBrains Mono', monospace; font-size: 11px;">${getConnectionSummary(r)}</span>`,
                    `<span class="badge" style="font-size: 10px;">${r.sensor_count || 0} sensors</span>`,
                    `<span class="badge badge-${r.status === 'active' ? 'success' : 'error'}">${r.status}</span>`,
                    `<div class="flex">
                        <button class="btn btn-primary btn-sm btn-view-sensors" data-id="${r.id}" data-name="${r.name}" data-conn="${getConnectionSummary(r)}">Sensors</button>
                        <button class="btn btn-ghost btn-sm btn-view-config" data-id="${r.id}" data-name="${r.name}">Config</button>
                    </div>`
                ]
            );

            document.querySelectorAll('.btn-view-sensors').forEach(btn => {
                btn.addEventListener('click', () => this.showSensors(btn.dataset.id, btn.dataset.name, btn.dataset.conn));
            });
            document.querySelectorAll('.btn-view-config').forEach(btn => {
                btn.addEventListener('click', () => this.showConfig(btn.dataset.id, btn.dataset.name));
            });

        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async showSensors(readerId, readerName, connSummary) {
        document.getElementById('modal-reader-name').textContent = `Sensors — ${readerName}`;
        document.getElementById('modal-reader-conn').textContent = connSummary;
        document.getElementById('sensors-modal').classList.remove('hidden');
        document.getElementById('sensors-table-container').innerHTML = '<div class="text-center page-subtitle">Loading sensors...</div>';

        try {
            const sensors = await API.getReaderSensors(readerId);
            if (!sensors || sensors.length === 0) {
                document.getElementById('sensors-table-container').innerHTML =
                    '<div class="text-center page-subtitle">No sensors configured for this reader.</div>';
                return;
            }
            Components.renderTable(
                ['Sensor Name', 'Template', 'Output', 'Status', 'Config Preview', 'Actions'],
                sensors,
                'sensors-table-container',
                (s) => [
                    `<b>${s.name}</b>`,
                    `<span style="font-size: 11px; color: var(--text-dim);">${s.template_name || 'custom'}</span>`,
                    `<span class="badge" style="font-size: 9px; background: rgba(255,255,255,0.05);">${s.output}</span>`,
                    `<span class="badge badge-${s.status === 'active' ? 'success' : 'error'}">${s.status}</span>`,
                    `<pre style="font-size: 10px; max-width: 240px; max-height: 80px; overflow: auto; background: rgba(0,0,0,0.2); padding: 5px; border-radius: 4px; margin: 0;">${JSON.stringify(s.config_json, null, 2)}</pre>`,
                    `<button class="btn btn-ghost btn-sm" onclick="window.location.hash='#telemetry?sensor_id=${s.id}'">Live Data</button>`
                ]
            );
        } catch (err) {
            document.getElementById('sensors-table-container').innerHTML =
                `<div class="text-center page-subtitle" style="color: var(--error);">${err.message}</div>`;
        }
    },

    showConfig(readerId, readerName) {
        const r = this._allReaders.find(x => x.id === readerId);
        if (!r) return;
        document.getElementById('reader-config-name').textContent = readerName;
        document.getElementById('reader-config-json').textContent = JSON.stringify(r.config_json, null, 2);
        document.getElementById('reader-config-modal').classList.remove('hidden');
    }
};

export default Readers;
