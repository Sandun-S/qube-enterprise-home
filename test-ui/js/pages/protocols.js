import API from '../api.js';
import Components from '../components.js';

const PROTO_ICONS = { modbus_tcp: '⚡', snmp: '🌐', mqtt: '📡', opcua: '🏭', http: '🔗', bacnet: '🏢', lorawan: '📶', dnp3: '🔌' };

const Protocols = {
    _user: null,

    async render() {
        this._user = await API.getMe();
        const isSuperadmin = this._user.role === 'superadmin';
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Protocols</h2>
                    <p class="page-subtitle">Supported communication standards and reader containers</p>
                </div>
                ${isSuperadmin ? '<button class="btn btn-primary" id="btn-add-protocol">+ Add Protocol</button>' : ''}
            </div>

            <div class="card">
                <div class="table-container">
                    <table>
                        <thead>
                            <tr>
                                <th>Protocol</th>
                                <th>ID</th>
                                <th>Description</th>
                                <th>Reader Standard</th>
                                <th>Status</th>
                                ${isSuperadmin ? '<th>Actions</th>' : ''}
                            </tr>
                        </thead>
                        <tbody id="protocols-tbody">
                            <tr><td colspan="6" class="text-center page-subtitle">Loading...</td></tr>
                        </tbody>
                    </table>
                </div>
            </div>

            <!-- Add / Edit Modal -->
            <div id="proto-modal" class="modal-backdrop hidden">
                <div class="modal">
                    <div class="modal-header">
                        <h2 id="proto-modal-title">Add Protocol</h2>
                    </div>
                    <div class="modal-body">
                        <form id="proto-form">
                            <input type="hidden" id="proto-edit-id">
                            <div class="form-group">
                                <label>Protocol ID <span style="color:var(--error)">*</span></label>
                                <input type="text" id="proto-id" placeholder="e.g. bacnet (lowercase, underscores)">
                                <small class="page-subtitle" style="font-size:11px;">Used as FK in readers/templates — cannot change after creation</small>
                            </div>
                            <div class="form-group">
                                <label>Display Label <span style="color:var(--error)">*</span></label>
                                <input type="text" id="proto-label" placeholder="e.g. BACnet/IP">
                            </div>
                            <div class="form-group">
                                <label>Description</label>
                                <input type="text" id="proto-description" placeholder="e.g. Building automation protocol">
                            </div>
                            <div class="form-group">
                                <label>Reader Standard</label>
                                <select id="proto-standard">
                                    <option value="endpoint">endpoint — one reader per device/gateway</option>
                                    <option value="multi_target">multi_target — one reader handles many devices</option>
                                </select>
                            </div>
                            <div class="form-group" id="proto-active-group" style="display:none;">
                                <label>
                                    <input type="checkbox" id="proto-active" style="width:auto;margin-right:8px;">
                                    Active (visible to users)
                                </label>
                            </div>
                        </form>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" id="btn-proto-cancel">Cancel</button>
                        <button class="btn btn-primary" id="btn-proto-save">Save</button>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this._user = this._user || await API.getMe();
        await this.loadProtocols();

        document.getElementById('btn-add-protocol')?.addEventListener('click', () => this._openAddModal());
        document.getElementById('btn-proto-cancel')?.addEventListener('click', () => this._closeModal());
        document.getElementById('btn-proto-save')?.addEventListener('click', () => this._handleSave());
        document.getElementById('proto-modal')?.addEventListener('click', (e) => {
            if (e.target.id === 'proto-modal') this._closeModal();
        });
    },

    async loadProtocols() {
        const isSuperadmin = this._user?.role === 'superadmin';
        try {
            let protocols;
            if (isSuperadmin) {
                protocols = await API.getAllProtocolsAdmin();
            } else {
                protocols = await API.getProtocols();
            }

            const tbody = document.getElementById('protocols-tbody');
            if (!protocols.length) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center page-subtitle">No protocols found</td></tr>`;
                return;
            }

            tbody.innerHTML = protocols.map(p => `
                <tr>
                    <td>
                        <span style="font-size:18px;margin-right:8px;">${PROTO_ICONS[p.id] || '🔧'}</span>
                        <strong>${p.label}</strong>
                    </td>
                    <td><code class="badge badge-blue" style="font-size:11px;">${p.id}</code></td>
                    <td class="page-subtitle" style="font-size:12px;">${p.description || '—'}</td>
                    <td><span class="badge ${p.reader_standard === 'multi_target' ? 'badge-success' : 'badge-blue'}" style="font-size:10px;">${p.reader_standard}</span></td>
                    <td><span class="badge ${p.is_active !== false ? 'badge-success' : 'badge-error'}" style="font-size:10px;">${p.is_active !== false ? 'ACTIVE' : 'INACTIVE'}</span></td>
                    ${isSuperadmin ? `
                    <td>
                        <button class="btn btn-ghost btn-sm btn-edit-proto" data-id="${p.id}">✏️ Edit</button>
                        <button class="btn btn-ghost btn-sm btn-delete-proto" data-id="${p.id}">🗑️</button>
                    </td>` : ''}
                </tr>
            `).join('');

            tbody.querySelectorAll('.btn-edit-proto').forEach(btn => {
                const proto = protocols.find(p => p.id === btn.dataset.id);
                btn.addEventListener('click', () => this._openEditModal(proto));
            });
            tbody.querySelectorAll('.btn-delete-proto').forEach(btn => {
                btn.addEventListener('click', () => this._handleDelete(btn.dataset.id));
            });

        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    _openAddModal() {
        document.getElementById('proto-form').reset();
        document.getElementById('proto-edit-id').value = '';
        document.getElementById('proto-modal-title').textContent = 'Add Protocol';
        document.getElementById('proto-id').disabled = false;
        document.getElementById('proto-active-group').style.display = 'none';
        document.getElementById('proto-modal').classList.remove('hidden');
    },

    _openEditModal(proto) {
        document.getElementById('proto-edit-id').value = proto.id;
        document.getElementById('proto-id').value = proto.id;
        document.getElementById('proto-id').disabled = true;
        document.getElementById('proto-label').value = proto.label;
        document.getElementById('proto-description').value = proto.description || '';
        document.getElementById('proto-standard').value = proto.reader_standard;
        document.getElementById('proto-active').checked = proto.is_active !== false;
        document.getElementById('proto-active-group').style.display = 'block';
        document.getElementById('proto-modal-title').textContent = 'Edit Protocol';
        document.getElementById('proto-modal').classList.remove('hidden');
    },

    _closeModal() {
        document.getElementById('proto-modal').classList.add('hidden');
    },

    async _handleSave() {
        const editId = document.getElementById('proto-edit-id').value;
        const isEdit = !!editId;

        const body = {
            id: document.getElementById('proto-id').value.trim(),
            label: document.getElementById('proto-label').value.trim(),
            description: document.getElementById('proto-description').value.trim(),
            reader_standard: document.getElementById('proto-standard').value,
            is_active: document.getElementById('proto-active').checked,
        };

        if (!body.label) {
            Components.showAlert('Label is required', 'error');
            return;
        }
        if (!isEdit && !body.id) {
            Components.showAlert('Protocol ID is required', 'error');
            return;
        }

        try {
            if (isEdit) {
                await API.updateProtocol(editId, body);
                Components.showAlert('Protocol updated');
            } else {
                await API.createProtocol(body);
                Components.showAlert('Protocol created');
            }
            this._closeModal();
            await this.loadProtocols();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async _handleDelete(id) {
        if (!confirm(`Delete protocol "${id}"? This will fail if any readers use it.`)) return;
        try {
            await API.deleteProtocol(id);
            Components.showAlert('Protocol deleted');
            await this.loadProtocols();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

export default Protocols;
