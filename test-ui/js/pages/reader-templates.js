import API from '../api.js';
import Components from '../components.js';

const ReaderTemplates = {
    async render() {
        const user = await API.getMe();
        const templates = await API.get('/api/v1/reader-templates');
        const protocols = await API.getProtocols();

        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">Reader Templates</h1>
                    <p class="page-subtitle">Configure connection schemas and protocol standards</p>
                </div>
                <div class="card-header-actions">
                    ${user.role === 'superadmin' ? '<button id="btn-add-rt" class="btn btn-primary">➕ Add Reader Template</button>' : ''}
                </div>
            </div>

            <div class="card">
                <div class="table-container">
                    <table>
                        <thead>
                            <tr>
                                <th>Protocol</th>
                                <th>Name</th>
                                <th>Image Suffix</th>
                                <th>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${templates.map(t => `
                                <tr>
                                    <td><span class="badge badge-blue">${t.protocol.toUpperCase()}</span></td>
                                    <td><strong>${t.name}</strong></td>
                                    <td><code>${t.image_suffix}</code></td>
                                    <td>
                                        <button class="btn btn-ghost btn-sm btn-edit-rt" data-id="${t.id}">✏️ Edit</button>
                                        ${user.role === 'superadmin' ? `<button class="btn btn-ghost btn-sm btn-delete-rt" data-id="${t.id}">🗑️ Delete</button>` : ''}
                                    </td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
                
                <div class="mt-20 flex-between">
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-raw">👁️ View Raw JSON</button>
                    <span class="text-dim">${templates.length} templates installed</span>
                </div>
                <pre id="raw-rt-json" class="raw-json-preview hidden">${JSON.stringify(templates, null, 2)}</pre>
            </div>

            <!-- Add/Edit Modal (Template) -->
            <div id="rt-modal" class="modal-backdrop hidden">
                <div class="modal">
                    <div class="modal-header">
                        <h2 id="modal-title">Add Reader Template</h2>
                    </div>
                    <div class="modal-body">
                        <form id="rt-form">
                            <input type="hidden" id="rt-id">
                            <div class="form-group">
                                <label>Protocol</label>
                                <select id="rt-protocol" required>
                                    ${protocols.map(p => `<option value="${p.id}">${p.label}</option>`).join('')}
                                </select>
                            </div>
                            <div class="form-group">
                                <label>Template Name</label>
                                <input type="text" id="rt-name" placeholder="Modbus TCP Reader" required>
                            </div>
                            <div class="form-group">
                                <label>Image Suffix</label>
                                <input type="text" id="rt-image" placeholder="modbus-reader" required>
                            </div>
                            <div class="form-group">
                                <label>Connection Schema (JSON Schema)</label>
                                <textarea id="rt-schema" style="height: 200px; font-family: var(--font-mono); font-size: 11px;">{
  "type": "object",
  "properties": {
    "host": {"type": "string", "title": "Host Address"},
    "port": {"type": "integer", "title": "Port", "default": 502}
  },
  "required": ["host"]
}</textarea>
                            </div>
                            <div class="form-group">
                                <label>Env Defaults (JSON)</label>
                                <textarea id="rt-env" style="height: 80px; font-family: var(--font-mono); font-size: 11px;">{"LOG_LEVEL": "info"}</textarea>
                            </div>
                        </form>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" onclick="document.getElementById('rt-modal').classList.add('hidden')">Cancel</button>
                        <button class="btn btn-primary" id="btn-save-rt">Save Template</button>
                    </div>
                </div>
            </div>
        `;
    },

    async init() {
        this.bindEvents();
    },

    bindEvents() {
        const btnAdd = document.getElementById('btn-add-rt');
        const btnSave = document.getElementById('btn-save-rt');
        const modal = document.getElementById('rt-modal');
        const btnToggleRaw = document.getElementById('btn-toggle-raw');

        btnAdd?.addEventListener('click', () => {
            document.getElementById('rt-form').reset();
            document.getElementById('rt-id').value = '';
            document.getElementById('modal-title').textContent = 'Add Reader Template';
            modal.classList.remove('hidden');
        });

        document.querySelectorAll('.btn-edit-rt').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const id = e.target.closest('button').dataset.id;
                this.loadTemplate(id);
            });
        });

        document.querySelectorAll('.btn-delete-rt').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const id = e.target.closest('button').dataset.id;
                if (confirm('Are you sure you want to delete this reader template?')) {
                    this.handleDelete(id);
                }
            });
        });

        btnSave?.addEventListener('click', () => this.handleSave());

        btnToggleRaw.addEventListener('click', () => {
            document.getElementById('raw-rt-json').classList.toggle('hidden');
        });
    },

    async loadTemplate(id) {
        try {
            const t = await API.get(`/api/v1/reader-templates/${id}`);
            document.getElementById('rt-id').value = t.id;
            document.getElementById('rt-protocol').value = t.protocol;
            document.getElementById('rt-name').value = t.name;
            document.getElementById('rt-image').value = t.image_suffix;
            document.getElementById('rt-schema').value = JSON.stringify(t.connection_schema, null, 2);
            document.getElementById('rt-env').value = JSON.stringify(t.env_defaults, null, 2);
            
            document.getElementById('modal-title').textContent = 'Edit Reader Template';
            document.getElementById('rt-modal').classList.remove('hidden');
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async handleSave() {
        const id = document.getElementById('rt-id').value;
        const body = {
            protocol: document.getElementById('rt-protocol').value,
            name: document.getElementById('rt-name').value,
            image_suffix: document.getElementById('rt-image').value,
            connection_schema: JSON.parse(document.getElementById('rt-schema').value),
            env_defaults: JSON.parse(document.getElementById('rt-env').value)
        };

        try {
            if (id) {
                await API.put(`/api/v1/reader-templates/${id}`, body);
                Components.showAlert('Reader template updated');
            } else {
                await API.post('/api/v1/reader-templates', body);
                Components.showAlert('Reader template created');
            }
            window.location.reload();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async handleDelete(id) {
        try {
            await API.delete(`/api/v1/reader-templates/${id}`);
            Components.showAlert('Reader template deleted');
            window.location.reload();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

export default ReaderTemplates;
