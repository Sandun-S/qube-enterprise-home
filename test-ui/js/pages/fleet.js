import API from '../api.js';
import Components from '../components.js';

const Fleet = {
    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Manage Fleet</h2>
                    <p class="page-subtitle">View and claim edge gateway devices</p>
                </div>
                <button id="btn-claim-modal" class="btn btn-primary">+ Claim New Qube</button>
            </div>

            <div class="card">
                <div class="card-title">All Qubes in Organization</div>
                <div id="fleet-table-container">
                    <div class="text-center page-subtitle">Loading fleet data...</div>
                </div>
            </div>

            <!-- Claim Modal -->
            <div id="claim-modal" class="modal-backdrop hidden">
                <div class="modal">
                    <div class="modal-header">
                        <h2 style="font-size: 18px;">Claim a New Qube</h2>
                    </div>
                    <div class="modal-body">
                        <div class="form-group">
                            <label>Register Key</label>
                            <input type="text" id="claim-reg-key" placeholder="e.g. TEST-Q1001-REG">
                            <p class="page-subtitle" style="font-size: 11px; margin-top: 4px;">Found on the device box or in /boot/mit.txt</p>
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-claim-cancel" class="btn btn-ghost">Cancel</button>
                        <button id="btn-claim-submit" class="btn btn-primary">Claim Device</button>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this.loadFleet();

        // Modal Events
        document.getElementById('btn-claim-modal')?.addEventListener('click', () => {
            document.getElementById('claim-modal').classList.remove('hidden');
        });
        document.getElementById('btn-claim-cancel')?.addEventListener('click', () => {
            document.getElementById('claim-modal').classList.add('hidden');
        });
        document.getElementById('btn-claim-submit')?.addEventListener('click', () => this.handleClaim());
    },

    async loadFleet() {
        try {
            const qubes = await API.getQubes();
            
            const headerActions = `<div class="flex-between mb-20" style="padding: 0 16px;">
                <button class="btn btn-ghost btn-sm" id="btn-toggle-fleet-raw">👁️ View Raw JSON</button>
            </div>
            <pre id="fleet-raw-json" class="raw-json-preview hidden" style="margin: 16px;">${JSON.stringify(qubes, null, 2)}</pre>`;
            
            document.getElementById('fleet-table-container').innerHTML = headerActions + '<div id="fleet-table"></div>';

            document.getElementById('btn-toggle-fleet-raw').onclick = () => {
                document.getElementById('fleet-raw-json').classList.toggle('hidden');
            };

            const isSuperAdmin = API.userRole === 'superadmin';

            Components.renderTable(
                ['Qube ID', 'Location', 'Status', 'Sync', 'Last Seen', 'Actions'],
                qubes,
                'fleet-table',
                (q) => {
                    const isSynced = q.config_hash === q.desired_config_hash;
                    const unclaimBtn = isSuperAdmin
                        ? `<button class="btn btn-ghost btn-sm" style="color:var(--error);border-color:var(--error);" onclick="Fleet.handleUnclaim('${q.id}')">Unclaim</button>`
                        : '';
                    return [
                        `<span style="font-weight: 600;">${q.id}</span>`,
                        q.location_label || '<span style="color:var(--text-dim)">Not set</span>',
                        `<span class="badge badge-${q.status === 'online' ? 'success' : q.status === 'offline' ? 'error' : 'blue'}">${q.status}</span>`,
                        `<span class="badge ${isSynced ? 'badge-success' : 'badge-warning'}">
                            ${isSynced ? '✅ Synced' : '🔄 Pending'}
                        </span>`,
                        q.last_seen ? new Date(q.last_seen).toLocaleString() : 'Never',
                        `<div class="flex">
                            <button class="btn btn-ghost btn-sm" onclick="window.location.hash='#telemetry?qube_id=${q.id}'">Sensors</button>
                            <button class="btn btn-ghost btn-sm" onclick="alert('Config Hash: ' + '${q.config_hash}' + '\\nDesired Hash: ' + '${q.desired_config_hash}')">Hash</button>
                            ${unclaimBtn}
                        </div>`
                    ];
                }
            );
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async handleClaim() {
        const key = document.getElementById('claim-reg-key').value;
        try {
            const res = await API.claimQube(key);
            Components.showAlert(`Successfully claimed ${res.qube_id}!`, 'success');
            document.getElementById('claim-modal').classList.add('hidden');
            this.loadFleet();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async handleUnclaim(qubeId) {
        if (!confirm(`Unclaim ${qubeId}?\n\nThis will remove all readers, sensors and containers for this device and return it to the unclaimed pool.`)) return;
        try {
            const res = await API.unclaimQube(qubeId);
            Components.showAlert(res.message, 'success');
            this.loadFleet();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

// Expose globally so inline onclick handlers can call Fleet.handleUnclaim
window.Fleet = Fleet;

export default Fleet;
