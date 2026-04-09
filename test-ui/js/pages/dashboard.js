import API from '../api.js';

const Dashboard = {
    _user: null,

    async render() {
        this._user = await API.getMe();
        const isSuperadmin = this._user.role === 'superadmin';
        const isViewer = this._user.role === 'viewer';

        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">${isSuperadmin ? 'System Overview' : 'Dashboard'}</h1>
                    <p class="page-subtitle">${isSuperadmin
                        ? 'Global platform status — all organisations and Qubes'
                        : 'Your fleet status and quick actions'}</p>
                </div>
                <div class="flex">
                    <span id="ws-status" class="badge badge-blue hidden">● Live</span>
                </div>
            </div>

            <div class="grid grid-4">
                <div class="card">
                    <div class="card-title">Total Qubes</div>
                    <div id="stat-qubes" style="font-size: 32px; font-weight: 700;">-</div>
                    <div class="page-subtitle">${isSuperadmin ? 'All organisations' : 'Your fleet'}</div>
                </div>
                <div class="card">
                    <div class="card-title">Active Readers</div>
                    <div id="stat-readers" style="font-size: 32px; font-weight: 700; color: var(--secondary);">-</div>
                    <div class="page-subtitle">Protocol containers</div>
                </div>
                <div class="card">
                    <div class="card-title">Monitored Devices</div>
                    <div id="stat-sensors" style="font-size: 32px; font-weight: 700; color: var(--accent);">-</div>
                    <div class="page-subtitle">Sensors configured</div>
                </div>
                <div class="card">
                    <div class="card-title">Online Now</div>
                    <div id="stat-online" style="font-size: 32px; font-weight: 700; color: var(--success);">-</div>
                    <div class="page-subtitle">Qubes connected</div>
                </div>
            </div>

            <!-- Quick actions — shown to non-viewer users -->
            ${!isViewer ? `
            <div class="card mt-20">
                <div class="card-title">Quick Actions</div>
                <div class="flex" style="gap: 12px; flex-wrap: wrap; margin-top: 12px;">
                    ${!isSuperadmin ? `
                    <a href="#onboarding" class="btn btn-primary">🪄 Add Device</a>
                    <a href="#fleet" class="btn btn-secondary">📦 Manage Fleet</a>
                    <a href="#commands" class="btn btn-ghost">⌨️ Send Command</a>
                    ` : `
                    <a href="#protocols" class="btn btn-primary">🔌 Manage Protocols</a>
                    <a href="#reader-templates" class="btn btn-secondary">📑 Reader Templates</a>
                    <a href="#templates" class="btn btn-ghost">📋 Device Templates</a>
                    <a href="#admin-qubes" class="btn btn-ghost">🖥️ All Qubes</a>
                    <a href="#registry" class="btn btn-ghost">🐳 Registry</a>
                    `}
                </div>
            </div>` : ''}

            <div class="grid grid-2 mt-20">
                <div class="card">
                    <div class="card-title">Fleet Status</div>
                    <div id="qube-status-list">
                        <div class="text-center page-subtitle">Loading...</div>
                    </div>
                </div>
                <div class="card">
                    <div class="card-title">Activity Log</div>
                    <div id="activity-log" style="font-family: 'JetBrains Mono', monospace; font-size: 12px; height: 300px; overflow-y: auto;">
                        <div class="page-subtitle">Awaiting events...</div>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this._user = this._user || await API.getMe();
        try {
            const qubes = await API.getQubes();
            document.getElementById('stat-qubes').textContent = qubes.length;
            document.getElementById('stat-online').textContent = qubes.filter(q => q.status === 'online').length;

            const qubeStatusList = document.getElementById('qube-status-list');
            qubeStatusList.innerHTML = '';

            const qubeDataPromises = qubes.map(async (qube) => {
                try {
                    const readers = await API.getQubeReaders(qube.id);
                    const readersWithSensors = await Promise.all(readers.map(async (reader) => {
                        try {
                            const sensors = await API.getReaderSensors(reader.id);
                            return { ...reader, sensors };
                        } catch (e) {
                            return { ...reader, sensors: [] };
                        }
                    }));
                    return { ...qube, readers: readersWithSensors };
                } catch (e) {
                    return { ...qube, readers: [] };
                }
            });

            const qubeResults = await Promise.all(qubeDataPromises);

            let totalReaders = 0;
            let totalSensors = 0;

            for (const qube of qubeResults) {
                totalReaders += qube.readers.length;

                const row = document.createElement('div');
                row.className = 'flex-between';
                row.style.cssText = 'padding: 10px 0; border-bottom: 1px solid var(--border);';
                row.innerHTML = `
                    <div class="flex">
                        <span style="width:8px;height:8px;border-radius:50%;background:${qube.status === 'online' ? 'var(--success)' : 'var(--text-dim)'};display:inline-block;margin-right:8px;"></span>
                        <span style="font-weight:600;">${qube.id}</span>
                        <span class="page-subtitle" style="font-size:11px;margin-left:8px;">${qube.location_label || '—'}</span>
                    </div>
                    <div style="display:flex;gap:8px;align-items:center;">
                        <span class="badge badge-${qube.status === 'online' ? 'success' : qube.status === 'offline' ? 'error' : 'blue'}" style="font-size:10px;">${qube.status}</span>
                        <span class="page-subtitle" style="font-size:11px;">${qube.readers.length} reader${qube.readers.length !== 1 ? 's' : ''}</span>
                    </div>
                `;
                qubeStatusList.appendChild(row);

                for (const reader of qube.readers) {
                    totalSensors += (reader.sensors || []).length;
                }
            }

            if (qubes.length === 0) {
                qubeStatusList.innerHTML = `<div class="text-center page-subtitle" style="padding:20px;">No Qubes in fleet yet. <a href="#fleet" style="color:var(--primary);">Go to Fleet →</a></div>`;
            }

            document.getElementById('stat-readers').textContent = totalReaders;
            document.getElementById('stat-sensors').textContent = totalSensors;

            this.logActivity('Dashboard loaded.');
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
