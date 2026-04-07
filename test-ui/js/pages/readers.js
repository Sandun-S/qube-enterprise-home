import API from '../api.js';
import Components from '../components.js';

const Readers = {
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

            <!-- Sensors Modal (View-only for now) -->
            <div id="sensors-modal" class="modal-backdrop hidden">
                <div class="modal" style="max-width: 800px;">
                    <div class="modal-header">
                        <h2 id="modal-reader-name" style="font-size: 18px;">Sensors</h2>
                    </div>
                    <div class="modal-body">
                        <div id="sensors-table-container"></div>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-modal-close" class="btn btn-ghost">Close</button>
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
    },

    async loadReaders() {
        try {
            const qubes = await API.getQubes();
            let allReaders = [];
            
            for (const qube of qubes) {
                const readers = await API.getQubeReaders(qube.id);
                allReaders = allReaders.concat(readers.map(r => ({ ...r, qubeId: qube.id })));
            }

            const headerActions = `<div class="flex-between mb-20" style="padding: 0 16px;">
                <button class="btn btn-ghost btn-sm" id="btn-toggle-readers-raw">👁️ View Raw JSON</button>
            </div>
            <pre id="readers-raw-json" class="raw-json-preview hidden" style="margin: 16px;">${JSON.stringify(allReaders, null, 2)}</pre>`;
            
            document.getElementById('readers-table-container').innerHTML = headerActions + '<div id="readers-table"></div>';

            document.getElementById('btn-toggle-readers-raw').onclick = () => {
                document.getElementById('readers-raw-json').classList.toggle('hidden');
            };

            Components.renderTable(
                ['Reader Name', 'Protocol', 'Qube', 'Host/Endpoint', 'Status', 'Actions'],
                allReaders,
                'readers-table',
                (r) => [
                    `<b>${r.name}</b>`,
                    `<span class="badge badge-blue">${r.protocol}</span>`,
                    `<code>${r.qubeId}</code>`,
                    `<span style="font-family: 'JetBrains Mono', monospace; font-size: 11px;">${r.config_json.host || r.config_json.endpoint || 'N/A'}</span>`,
                    `<span class="badge badge-${r.status === 'active' ? 'success' : 'error'}">${r.status}</span>`,
                    `<div class="flex">
                        <button class="btn btn-ghost btn-sm btn-view-sensors" data-id="${r.id}" data-name="${r.name}">View ${r.sensor_count || 0} Sensors</button>
                        <button class="btn btn-ghost btn-sm" onclick="alert(JSON.stringify(API.state.readers.find(x => x.id === '${r.id}'), null, 2))">JSON</button>
                    </div>`
                ]
            );
            API.state.readers = allReaders; // Cache for JSON view

            // Bind sensor buttons
            document.querySelectorAll('.btn-view-sensors').forEach(btn => {
                btn.addEventListener('click', () => this.showSensors(btn.dataset.id, btn.dataset.name));
            });

        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async showSensors(readerId, readerName) {
        document.getElementById('modal-reader-name').textContent = `Sensors for Reader: ${readerName}`;
        document.getElementById('sensors-modal').classList.remove('hidden');
        document.getElementById('sensors-table-container').innerHTML = '<div class="text-center page-subtitle">Loading sensors...</div>';

        try {
            const sensors = await API.getReaderSensors(readerId);
            Components.renderTable(
                ['Sensor Name', 'Status', 'Config (Merged)', 'Actions'],
                sensors,
                'sensors-table-container',
                (s) => [
                    `<b>${s.name}</b>`,
                    `<span class="badge badge-${s.status === 'active' ? 'success' : 'error'}">${s.status}</span>`,
                    `<pre style="font-size: 10px; max-width: 200px; max-height: 80px; overflow: auto; background: rgba(0,0,0,0.2); padding: 5px; border-radius: 4px;">${JSON.stringify(s.config_json, null, 2)}</pre>`,
                    `<button class="btn btn-ghost btn-sm" onclick="window.location.hash='#telemetry?sensor_id=${s.id}'">Live Data</button>`
                ]
            );
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

export default Readers;
