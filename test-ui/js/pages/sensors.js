import API from '../api.js';
import Components from '../components.js';

const Sensors = {
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
        `;
    },

    async init() {
        this.loadSensors();
    },

    async loadSensors() {
        try {
            const qubes = await API.getQubes();
            let allSensors = [];
            
            for (const qube of qubes) {
                const readers = await API.getQubeReaders(qube.id);
                for (const r of readers) {
                    const sensors = await API.getReaderSensors(r.id);
                    allSensors = allSensors.concat(sensors.map(s => ({ ...s, readerName: r.name, qubeId: qube.id })));
                }
            }

            const headerActions = `<div class="flex-between mb-20" style="padding: 0 16px;">
                <button class="btn btn-ghost btn-sm" id="btn-toggle-sensors-raw">👁️ View Raw JSON</button>
            </div>
            <pre id="sensors-raw-json" class="raw-json-preview hidden" style="margin: 16px;">${JSON.stringify(allSensors, null, 2)}</pre>`;
            
            document.getElementById('sensors-table-container').innerHTML = headerActions + '<div id="sensors-table"></div>';

            document.getElementById('btn-toggle-sensors-raw').onclick = () => {
                document.getElementById('sensors-raw-json').classList.toggle('hidden');
            };

            Components.renderTable(
                ['Sensor Name', 'Qube', 'Reader', 'Measurement', 'Status', 'Actions'],
                allSensors,
                'sensors-table',
                (s) => [
                    `<b>${s.name}</b>`,
                    `<code>${s.qubeId}</code>`,
                    `<span>${s.readerName}</span>`,
                    `<span class="badge badge-blue">${s.table_name || 'Measurements'}</span>`,
                    `<span class="badge badge-${s.status === 'active' ? 'success' : 'error'}">${s.status}</span>`,
                    `<div class="flex"><button class="btn btn-primary btn-sm" onclick="window.location.hash='#telemetry?sensor_id=${s.id}'">Live</button>
                     <button class="btn btn-ghost btn-sm" onclick="alert(JSON.stringify(API.state.sensors.find(x => x.id === '${s.id}'), null, 2))">JSON</button></div>`
                ]
            );
            API.state.sensors = allSensors; // Cache for JSON view
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

export default Sensors;
