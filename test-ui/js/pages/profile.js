import API from '../api.js';
import Components from '../components.js';

const Profile = {
    async render() {
        const user = await API.getMe();
        
        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">My Profile</h1>
                    <p class="page-subtitle">Manage your personal settings and view organization details</p>
                </div>
            </div>

            <div class="grid grid-2">
                <div class="card">
                    <h2 class="card-title">👤 User Details</h2>
                    <div class="form-group">
                        <label>Email Address</label>
                        <input type="text" value="${user.email}" readonly style="background: rgba(0,0,0,0.1); border-color: transparent;">
                    </div>
                    <div class="form-group">
                        <label>User ID</label>
                        <code class="raw-json-preview" style="display: block; margin-top: 0; padding: 8px;">${user.id}</code>
                    </div>
                    <div class="form-group">
                        <label>Role</label>
                        <span class="badge ${user.role === 'superadmin' ? 'badge-warning' : 'badge-blue'}">${user.role.toUpperCase()}</span>
                    </div>
                    
                    <button class="btn btn-ghost btn-sm" id="btn-logout" style="width: 100%; margin-top: 20px;">🚪 Sign Out of Session</button>
                </div>

                <div class="card">
                    <h2 class="card-title">🏢 Organisation</h2>
                    <div class="form-group">
                        <label>Organisation ID</label>
                        <code class="raw-json-preview" style="display: block; margin-top: 0; padding: 8px;">${user.org_id}</code>
                    </div>
                    <div class="form-group">
                        <label>Access Permissions</label>
                        <ul class="text-dim" style="font-size: 13px; padding-left: 20px;">
                            ${this.renderPermissions(user.role)}
                        </ul>
                    </div>
                </div>
            </div>

            <div class="card">
                <div class="flex-between">
                    <h2 class="card-title">🔑 Current Session (JWT)</h2>
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-token">👁️ View Token</button>
                </div>
                <div id="token-preview" class="hidden">
                    <pre class="raw-json-preview" style="white-space: pre-wrap; word-break: break-all;">${API.token}</pre>
                    <div class="mt-10 text-dim" style="font-size: 11px;">
                        Exp: ${this.getTokenExp(API.token)}
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        document.getElementById('btn-logout').addEventListener('click', () => API.logout());
        document.getElementById('btn-toggle-token').addEventListener('click', () => {
            document.getElementById('token-preview').classList.toggle('hidden');
        });
    },

    renderPermissions(role) {
        const perms = {
            superadmin: ['Full system access', 'Manage global templates', 'Configure Docker registry', 'Manage all organizations'],
            admin: ['Manage organization users', 'Claim new Qubes', 'Manage all readers & sensors'],
            editor: ['Create and edit readers', 'Create and edit sensors', 'Manage custom templates'],
            viewer: ['View dashboard and telemetry', 'Read-only access to fleet']
        };

        const list = perms[role] || perms.viewer;
        return list.map(p => `<li>${p}</li>`).join('');
    },

    getTokenExp(token) {
        try {
            const payload = JSON.parse(atob(token.split('.')[1]));
            return new Date(payload.exp * 1000).toLocaleString();
        } catch (e) {
            return 'Invalid Token';
        }
    }
};

export default Profile;
