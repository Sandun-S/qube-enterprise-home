import API from '../api.js';

const Dashboard = {
    async render() {
        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">Enterprise Dashboard</h1>
                    <p class="page-subtitle">Real-time overview of your Qube network</p>
                </div>
                <div class="flex">
                    <span id="sync-status" class="badge badge-success">● System Sync Active</span>
                </div>
            </div>

            <div class="grid grid-4">
                <div class="card">
                    <div class="card-title">Total Qubes</div>
                    <div id="stat-qubes" style="font-size: 32px; font-weight: 700;">-</div>
                    <div class="page-subtitle">Across all locations</div>
                </div>
                <div class="card">
                    <div class="card-title">Active Readers</div>
                    <div id="stat-readers" style="font-size: 32px; font-weight: 700; color: var(--secondary);">-</div>
                    <div class="page-subtitle">Protocol containers</div>
                </div>
                <div class="card">
                    <div class="card-title">Registered Sensors</div>
                    <div id="stat-sensors" style="font-size: 32px; font-weight: 700; color: var(--accent);">-</div>
                    <div class="page-subtitle">Physical devices</div>
                </div>
                <div class="card">
                    <div class="card-title">System Status</div>
                    <div style="font-size: 18px; font-weight: 700; color: var(--success); margin-top: 10px;">HEALTHY</div>
                    <div class="page-subtitle">No critical alerts</div>
                </div>
            </div>

            <div class="grid grid-2 mt-20">
                <div class="card">
                    <div class="card-title">Active Fleet Status</div>
                    <div id="qube-status-list">
                        <div class="text-center page-subtitle">Loading...</div>
                    </div>
                </div>
                <div class="card">
                    <div class="card-title">Recent Activity Log</div>
                    <div id="activity-log" style="font-family: 'JetBrains Mono', monospace; font-size: 12px; height: 300px; overflow-y: auto;">
                        <div class="page-subtitle">Awaiting event stream...</div>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        try {
            const qubes = await API.getQubes();
            document.getElementById('stat-qubes').textContent = qubes.length;
            
            const qubeStatusList = document.getElementById('qube-status-list');
            qubeStatusList.innerHTML = '';

            // Map each qube to a promise that fetches its readers and sensors in parallel
            const qubeDataPromises = qubes.map(async (qube) => {
                try {
                    const readers = await API.getQubeReaders(qube.id);
                    const readersWithSensors = await Promise.all(readers.map(async (reader) => {
                        try {
                            const sensors = await API.getReaderSensors(reader.id);
                            return { ...reader, sensors };
                        } catch (e) {
                            console.warn(`Failed to fetch sensors for reader ${reader.id}`, e);
                            return { ...reader, sensors: [] };
                        }
                    }));
                    return { ...qube, readers: readersWithSensors };
                } catch (e) {
                    console.warn(`Failed to fetch data for qube ${qube.id}`, e);
                    return { ...qube, readers: [] };
                }
            });

            const qubeResults = await Promise.all(qubeDataPromises);

            let totalReaders = 0;
            let totalSensors = 0;

            for (const qube of qubeResults) {
                totalReaders += qube.readers.length;
                
                // Render small status row
                const row = document.createElement('div');
                row.className = 'flex-between';
                row.style.padding = '10px 0';
                row.style.borderBottom = '1px solid var(--border)';
                row.innerHTML = `
                    <div class="flex">
                        <span class="dot ${qube.status === 'online' ? 'dot-online' : ''}" style="background: ${qube.status === 'online' ? 'var(--success)' : 'var(--text-dim)'}"></span>
                        <span style="font-weight: 600;">${qube.id}</span>
                        <span class="page-subtitle" style="font-size: 11px;">${qube.location_label || 'Default Location'}</span>
                    </div>
                    <div class="badge badge-${qube.status === 'online' ? 'success' : 'error'}">${qube.status}</div>
                `;
                qubeStatusList.appendChild(row);

                for (const reader of qube.readers) {
                    totalSensors += (reader.sensors || []).length;
                }
            }

            document.getElementById('stat-readers').textContent = totalReaders;
            document.getElementById('stat-sensors').textContent = totalSensors;

            this.logActivity('System initialized. Dashboard data updated.');
        } catch (err) {
            console.error('Dashboard init failed', err);
            this.logActivity(`Error: ${err.message}`, 'error');
            const qubeStatusList = document.getElementById('qube-status-list');
            if (qubeStatusList) {
                qubeStatusList.innerHTML = `<div class="badge badge-error" style="width:100%">${err.message}</div>`;
            }
        }
    },

    logActivity(msg, type = 'info') {
        const log = document.getElementById('activity-log');
        if (!log) return;
        const entry = document.createElement('div');
        entry.style.marginBottom = '4px';
        entry.style.color = type === 'error' ? 'var(--error)' : 'var(--text-main)';
        entry.innerHTML = `<span style="color: var(--text-dim)">[${new Date().toLocaleTimeString()}]</span> ${msg}`;
        log.prepend(entry);
    }
};

export default Dashboard;
