import API from '../api.js';
import Components from '../components.js';

const Templates = {
    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Device Templates</h2>
                    <p class="page-subtitle">Standardized measurement definitions for physical devices</p>
                </div>
                <button class="btn btn-primary" id="btn-create-template">+ Create New Template</button>
            </div>

            <div class="card">
                <div class="flex-between mb-20">
                    <div class="flex">
                        <span class="badge badge-blue">Global</span>
                        <span class="badge badge-success">Organization</span>
                    </div>
                    <div class="flex">
                        <select id="template-proto-filter" style="width: 150px;">
                            <option value="">All Protocols</option>
                        </select>
                    </div>
                </div>
                <div id="templates-grid" class="grid grid-3">
                    <div class="text-center page-subtitle">Loading device catalog...</div>
                </div>
            </div>
        `;
    },

    async init() {
        const protocols = await API.getProtocols();
        const filter = document.getElementById('template-proto-filter');
        protocols.forEach(p => {
            const opt = document.createElement('option');
            opt.value = p.id;
            opt.textContent = p.label;
            filter.appendChild(opt);
        });

        this.loadTemplates();
        filter.addEventListener('change', () => this.loadTemplates(filter.value));
    },

    async loadTemplates(protocol = '') {
        try {
            const templates = await API.getDeviceTemplates(protocol);
            const grid = document.getElementById('templates-grid');
            grid.innerHTML = '';

            const rawContainer = document.createElement('div');
            rawContainer.className = 'card';
            rawContainer.style.gridColumn = '1 / -1';
            rawContainer.innerHTML = `
                <div class="flex-between">
                    <h3 class="card-title">📜 Device Catalog Data</h3>
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-templates-raw">👁️ Toggle Raw JSON</button>
                </div>
                <pre id="templates-raw-json" class="raw-json-preview hidden">${JSON.stringify(templates, null, 2)}</pre>
            `;
            grid.appendChild(rawContainer);

            document.getElementById('btn-toggle-templates-raw').onclick = () => {
                document.getElementById('templates-raw-json').classList.toggle('hidden');
            };

            templates.forEach(t => {
                const card = document.createElement('div');
                card.className = 'card';
                card.style.position = 'relative';
                card.innerHTML = `
                    <div style="font-size: 11px; position: absolute; top: 15px; right: 20px;">
                        <span class="badge badge-${t.is_global ? 'blue' : 'success'}">${t.is_global ? 'GLOBAL' : 'PRIVATE'}</span>
                    </div>
                    <div style="font-weight: 700; margin-bottom: 4px;">${t.manufacturer} ${t.model}</div>
                    <div style="font-size: 13px; font-weight: 600; color: var(--primary); margin-bottom: 8px;">${t.name}</div>
                    <p class="page-subtitle" style="font-size: 11px; margin-bottom: 12px; height: 3.2em; overflow: hidden; line-height: 1.6;">${t.description}</p>
                    <div class="flex-between" style="border-top: 1px solid var(--border); padding-top: 12px;">
                        <span class="badge badge-blue" style="font-size: 9px; line-height: 1;">${t.protocol}</span>
                        <button class="btn btn-ghost btn-sm btn-edit-template" data-id="${t.id}">Edit</button>
                    </div>
                `;
                grid.appendChild(card);
            });

            document.querySelectorAll('.btn-edit-template').forEach(btn => {
                btn.onclick = (e) => this.showEditModal(e.target.dataset.id);
            });

        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    async showEditModal(id) {
        const t = await API.get(`/api/v1/device-templates/${id}`);
        // Simplified for now - in a real app this would be a full form
        const newName = prompt('Enter new template name:', t.name);
        if (newName && newName !== t.name) {
            try {
                await API.put(`/api/v1/device-templates/${id}`, { ...t, name: newName });
                Components.showAlert('Template updated');
                this.loadTemplates();
            } catch (err) {
                Components.showAlert(err.message, 'error');
            }
        }
    }
};

export default Templates;
