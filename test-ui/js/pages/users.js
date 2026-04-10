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

            <!-- Invite User Modal -->
            <div id="invite-modal" class="modal-backdrop hidden">
                <div class="modal">
                    <div class="modal-header">
                        <h2 style="font-size: 18px;">Invite Team Member</h2>
                    </div>
                    <div class="modal-body">
                        <div class="form-group">
                            <label>Email <span style="color:var(--error)">*</span></label>
                            <input type="email" id="invite-email" placeholder="user@example.com">
                        </div>
                        <div class="form-group">
                            <label>Password <span style="color:var(--text-dim); font-size:11px;">(leave blank to use default: Qube@2024)</span></label>
                            <input type="password" id="invite-password" placeholder="optional">
                        </div>
                        <div class="form-group">
                            <label>Role</label>
                            <select id="invite-role">
                                <option value="viewer">Viewer</option>
                                <option value="editor">Editor</option>
                                <option value="admin">Admin</option>
                            </select>
                        </div>
                        <div id="invite-superadmin-note" class="hidden" style="font-size:11px;color:var(--text-dim);margin-top:-8px;padding:8px;background:rgba(183,148,244,0.08);border-radius:6px;">
                            Superadmin users are added to the global admin org and have access to all tenant management tools.
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-invite-cancel" class="btn btn-ghost">Cancel</button>
                        <button id="btn-invite-submit" class="btn btn-primary">Invite</button>
                    </div>
                </div>
            </div>

            <!-- Edit Role Modal -->
            <div id="edit-role-modal" class="modal-backdrop hidden">
                <div class="modal">
                    <div class="modal-header">
                        <h2 style="font-size: 18px;">Change Role</h2>
                    </div>
                    <div class="modal-body">
                        <p class="page-subtitle" id="edit-role-user-email" style="margin-bottom:16px;"></p>
                        <div class="form-group">
                            <label>New Role</label>
                            <select id="edit-role-select">
                                <option value="viewer">Viewer</option>
                                <option value="editor">Editor</option>
                                <option value="admin">Admin</option>
                            </select>
                        </div>
                    </div>
                    <div class="modal-footer">
                        <button id="btn-edit-role-cancel" class="btn btn-ghost">Cancel</button>
                        <button id="btn-edit-role-submit" class="btn btn-primary">Save</button>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this.loadUsers();

        // Add superadmin option to role dropdown if current user is superadmin
        if (API.userRole === 'superadmin') {
            const roleSelect = document.getElementById('invite-role');
            const saOption = document.createElement('option');
            saOption.value = 'superadmin';
            saOption.textContent = 'Superadmin';
            roleSelect.appendChild(saOption);

            roleSelect.addEventListener('change', () => {
                const note = document.getElementById('invite-superadmin-note');
                if (roleSelect.value === 'superadmin') {
                    note.classList.remove('hidden');
                } else {
                    note.classList.add('hidden');
                }
            });
        }

        document.getElementById('btn-invite-user')?.addEventListener('click', () => {
            document.getElementById('invite-email').value = '';
            document.getElementById('invite-password').value = '';
            document.getElementById('invite-role').value = 'viewer';
            document.getElementById('invite-superadmin-note').classList.add('hidden');
            document.getElementById('invite-modal').classList.remove('hidden');
        });
        document.getElementById('btn-invite-cancel')?.addEventListener('click', () => {
            document.getElementById('invite-modal').classList.add('hidden');
        });
        document.getElementById('btn-invite-submit')?.addEventListener('click', () => this.handleInvite());

        document.getElementById('btn-edit-role-cancel')?.addEventListener('click', () => {
            document.getElementById('edit-role-modal').classList.add('hidden');
        });
    },

    async loadUsers() {
        try {
            const users = await API.getUsers();
            Components.renderTable(
                ['User Email', 'Role', 'Joined Date', 'Actions'],
                users,
                'users-table-container',
                (u) => [
                    `<div class="flex">
                        <div style="width:32px;height:32px;background:var(--border);border-radius:50%;display:flex;align-items:center;justify-content:center;font-weight:bold;font-size:12px;color:var(--text-dim);">
                            ${u.email.charAt(0).toUpperCase()}
                        </div>
                        <b>${u.email}</b>
                    </div>`,
                    `<span class="badge badge-${u.role === 'superadmin' ? 'warning' : u.role === 'admin' ? 'success' : 'blue'}">${u.role.toUpperCase()}</span>`,
                    new Date(u.created_at).toLocaleDateString(),
                    u.role === 'superadmin' ? '' : `
                        <button class="btn btn-ghost btn-sm" onclick="Users._openEditRole('${u.id}','${u.email}','${u.role}')">Edit Role</button>
                        <button class="btn btn-ghost btn-sm" style="color:var(--error)" onclick="Users._removeUser('${u.id}','${u.email}')">Remove</button>
                    `
                ]
            );
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async handleInvite() {
        const email = document.getElementById('invite-email').value.trim();
        const password = document.getElementById('invite-password').value;
        const role = document.getElementById('invite-role').value;

        if (!email) {
            Components.showAlert('Email is required', 'error');
            return;
        }

        try {
            const result = await API.inviteUser({ email, password: password || undefined, role });
            document.getElementById('invite-modal').classList.add('hidden');
            let msg = `${result.email} invited as ${result.role}`;
            if (result.is_temp_password) msg += ` (temp password: ${result.temp_password})`;
            Components.showAlert(msg, 'success');
            this.loadUsers();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    _openEditRole(userId, email, currentRole) {
        document.getElementById('edit-role-user-email').textContent = email;

        // Rebuild options so superadmin can also set superadmin role
        const sel = document.getElementById('edit-role-select');
        sel.innerHTML = `
            <option value="viewer">Viewer</option>
            <option value="editor">Editor</option>
            <option value="admin">Admin</option>
            ${API.userRole === 'superadmin' ? '<option value="superadmin">Superadmin</option>' : ''}
        `;
        sel.value = currentRole;

        document.getElementById('edit-role-modal').classList.remove('hidden');
        document.getElementById('btn-edit-role-submit').onclick = () => this._submitEditRole(userId);
    },

    async _submitEditRole(userId) {
        const role = document.getElementById('edit-role-select').value;
        try {
            await API.updateUserRole(userId, role);
            document.getElementById('edit-role-modal').classList.add('hidden');
            Components.showAlert('Role updated', 'success');
            this.loadUsers();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async _removeUser(userId, email) {
        if (!confirm(`Remove ${email} from the organization?`)) return;
        try {
            await API.removeUser(userId);
            Components.showAlert(`${email} removed`, 'success');
            this.loadUsers();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

// Expose for inline onclick handlers in table rows
window.Users = Users;

export default Users;
