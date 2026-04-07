import API from '../api.js';
import Components from '../components.js';

const Users = {
    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Team Management</h2>
                    <p class="page-subtitle">Manage users and access roles for your organization</p>
                </div>
                <button class="btn btn-primary" id="btn-invite-user">+ Invite Member</button>
            </div>

            <div class="card">
                <div class="card-title">Active Members</div>
                <div id="users-table-container">
                    <div class="text-center page-subtitle">Loading team members...</div>
                </div>
            </div>

            <div class="card" style="border-color: var(--accent); background: rgba(183, 148, 244, 0.05);">
                <div class="flex">
                    <div style="font-size: 24px;">🛡️</div>
                    <div>
                        <div style="font-weight: 700;">Role Permissions Guide</div>
                        <div class="page-subtitle" style="font-size: 12px;">
                            <b>Admin:</b> Full access to Qubes, Readers, Sensors and Teams.<br>
                            <b>Editor:</b> Can add/edit Sensors and Readers but cannot manage users.<br>
                            <b>Viewer:</b> Read-only access to Dashboards and Telemetry.
                        </div>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this.loadUsers();
    },

    async loadUsers() {
        try {
            const users = await API.getUsers();
            Components.renderTable(
                ['User Email', 'Role', 'Joined Date', 'Status', 'Actions'],
                users,
                'users-table-container',
                (u) => [
                    `<div class="flex">
                        <div style="width: 32px; height: 32px; background: var(--border); border-radius: 50%; display: flex; align-items: center; justify-content: center; font-weight: bold; font-size: 12px; color: var(--text-dim);">
                            ${u.email.charAt(0).toUpperCase()}
                        </div>
                        <b>${u.email}</b>
                    </div>`,
                    `<span class="badge badge-${u.role === 'admin' ? 'success' : u.role === 'superadmin' ? 'warning' : 'blue'}">${u.role.toUpperCase()}</span>`,
                    new Date(u.created_at).toLocaleDateString(),
                    `<span class="badge badge-success">ACTIVE</span>`,
                    `<button class="btn btn-ghost btn-sm">Edit Role</button>`
                ]
            );
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

export default Users;
