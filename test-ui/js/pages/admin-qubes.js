import API from '../api.js';
import Components from '../components.js';

const AdminQubes = {
    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Qube Management</h2>
                    <p class="page-subtitle">All claimed devices across all organisations — superadmin view</p>
                </div>
            </div>

            <div class="card">
                <div class="card-title">All Claimed Qubes</div>
                <div id="admin-qubes-container">
                    <div class="text-center page-subtitle">Loading...</div>
                </div>
            </div>
        `;
    },

    async init() {
        this.load();
    },

    async load() {
        try {
            const qubes = await API.getAllQubesAdmin();

            if (qubes.length === 0) {
                document.getElementById('admin-qubes-container').innerHTML =
                    '<div class="text-center page-subtitle" style="padding: 32px;">No claimed devices found.</div>';
                return;
            }

            const tableHTML = `
                <div style="overflow-x: auto;">
                    <table>
                        <thead>
                            <tr>
                                <th>Qube ID</th>
                                <th>Organisation</th>
                                <th>Location</th>
                                <th>Status</th>
                                <th>WS</th>
                                <th>Claimed At</th>
                                <th>Last Seen</th>
                                <th>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${qubes.map(q => `
                                <tr>
                                    <td><span style="font-weight:600;font-family:var(--font-mono)">${q.id}</span></td>
                                    <td>
                                        <div style="font-weight:600">${q.org_name}</div>
                                        <div style="font-size:11px;color:var(--text-dim)">${q.org_id}</div>
                                    </td>
                                    <td>${q.location_label || '<span style="color:var(--text-dim)">—</span>'}</td>
                                    <td>
                                        <span class="badge badge-${q.status === 'online' ? 'success' : q.status === 'offline' ? 'error' : 'blue'}">
                                            ${q.status}
                                        </span>
                                    </td>
                                    <td>
                                        <span class="badge ${q.ws_connected ? 'badge-success' : ''}">
                                            ${q.ws_connected ? '🟢 Yes' : '—'}
                                        </span>
                                    </td>
                                    <td style="font-size:12px">${q.claimed_at ? new Date(q.claimed_at).toLocaleString() : '—'}</td>
                                    <td style="font-size:12px">${q.last_seen ? new Date(q.last_seen).toLocaleString() : 'Never'}</td>
                                    <td>
                                        <button class="btn btn-ghost btn-sm"
                                            style="color:var(--error);border-color:var(--error);"
                                            onclick="AdminQubes.handleUnclaim('${q.id}', '${q.org_name}')">
                                            Unclaim
                                        </button>
                                    </td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
                <div style="padding: 12px 16px; font-size: 12px; color: var(--text-dim);">
                    ${qubes.length} claimed device${qubes.length !== 1 ? 's' : ''} across all organisations
                </div>
            `;

            document.getElementById('admin-qubes-container').innerHTML = tableHTML;
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async handleUnclaim(qubeId, orgName) {
        if (!confirm(`Unclaim ${qubeId} from "${orgName}"?\n\nThis will remove all readers, sensors and containers for this device and return it to the unclaimed pool.`)) return;
        try {
            const res = await API.unclaimQube(qubeId);
            Components.showAlert(res.message, 'success');
            this.load();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

window.AdminQubes = AdminQubes;

export default AdminQubes;
