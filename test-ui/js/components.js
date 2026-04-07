/**
 * Qube Enterprise Component Library
 * Reusable UI components for the management console.
 */

const Components = {
  /**
   * Generates a dynamic form based on a JSON Schema.
   * Supports: type (string, integer, etc.), format (ipv4, password), enum, default, required.
   */
  renderSchemaForm(schema, containerId, prefix = 'f') {
    const container = document.getElementById(containerId);
    if (!container || !schema || !schema.properties) return;

    container.innerHTML = '';
    const properties = schema.properties;
    const requiredFields = schema.required || [];

    for (const [key, prop] of Object.entries(properties)) {
      const fieldId = `${prefix}-${key}`;
      const formGroup = document.createElement('div');
      formGroup.className = 'form-group';

      const label = document.createElement('label');
      label.textContent = prop.title || key;
      if (requiredFields.includes(key)) {
        label.innerHTML += ' <span style="color:var(--error)">*</span>';
      }
      formGroup.appendChild(label);

      let input;
      if (prop.enum) {
        input = document.createElement('select');
        input.id = fieldId;
        prop.enum.forEach(option => {
          const opt = document.createElement('option');
          opt.value = option;
          opt.textContent = option;
          if (option === prop.default) opt.selected = true;
          input.appendChild(opt);
        });
      } else {
        input = document.createElement(prop.type === 'string' && prop.format !== 'password' ? 'input' : 'input');
        input.id = fieldId;
        input.placeholder = prop.description || '';
        
        if (prop.type === 'integer' || prop.type === 'number') {
          input.type = 'number';
          if (prop.minimum !== undefined) input.min = prop.minimum;
          if (prop.maximum !== undefined) input.max = prop.maximum;
        } else if (prop.format === 'password') {
          input.type = 'password';
        } else {
          input.type = 'text';
        }

        if (prop.default !== undefined) {
          input.value = prop.default;
        }
      }

      formGroup.appendChild(input);

      if (prop.description) {
        const hint = document.createElement('div');
        hint.className = 'page-subtitle';
        hint.style.fontSize = '12px';
        hint.style.marginTop = '4px';
        hint.textContent = prop.description;
        formGroup.appendChild(hint);
      }

      container.appendChild(formGroup);
    }
  },

  /**
   * Collects values from a schema-generated form.
   */
  collectSchemaValues(schema, prefix = 'f') {
    const values = {};
    if (!schema || !schema.properties) return values;

    for (const [key, prop] of Object.entries(schema.properties)) {
      const el = document.getElementById(`${prefix}-${key}`);
      if (!el) continue;

      let val = el.value;
      if (prop.type === 'integer') val = parseInt(val, 10);
      else if (prop.type === 'number') val = parseFloat(val);
      
      values[key] = val;
    }
    return values;
  },

  /**
   * Renders a standardized data table.
   */
  renderTable(headers, data, containerId, rowMapper) {
    const container = document.getElementById(containerId);
    if (!container) return;

    if (!data || data.length === 0) {
      container.innerHTML = '<div class="text-center page-subtitle">No data available</div>';
      return;
    }

    const tableContainer = document.createElement('div');
    tableContainer.className = 'table-container';

    const table = document.createElement('table');
    
    // Header
    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    headers.forEach(h => {
      const th = document.createElement('th');
      th.textContent = h;
      headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);
    table.appendChild(thead);

    // Body
    const tbody = document.createElement('tbody');
    data.forEach(item => {
      const tr = document.createElement('tr');
      const cells = rowMapper(item);
      cells.forEach(cell => {
        const td = document.createElement('td');
        if (cell instanceof HTMLElement) td.appendChild(cell);
        else td.innerHTML = cell;
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    });
    table.appendChild(tbody);

    tableContainer.appendChild(table);
    container.innerHTML = '';
    container.appendChild(tableContainer);
  },

  /**
   * Alerts
   */
  showAlert(message, type = 'success') {
    const alert = document.createElement('div');
    alert.style.position = 'fixed';
    alert.style.bottom = '20px';
    alert.style.right = '20px';
    alert.style.zIndex = '2000';
    alert.className = `badge badge-${type}`;
    alert.style.padding = '12px 24px';
    alert.style.boxShadow = '0 10px 30px rgba(0,0,0,0.5)';
    alert.innerText = message;
    
    document.body.appendChild(alert);
    
    setTimeout(() => {
      alert.style.transition = '0.5s opacity';
      alert.style.opacity = '0';
      setTimeout(() => alert.remove(), 500);
    }, 3000);
  },

  /**
   * Simple JSON Editor
   */
  renderJsonEditor(data, containerId, title = 'Raw Configuration') {
    const container = document.getElementById(containerId);
    if (!container) return;
    
    container.innerHTML = `
      <div class="form-group">
        <label>${title}</label>
        <textarea id="json-editor-area" style="height: 300px; font-family: 'JetBrains Mono', monospace; font-size: 11px; background: #1a1d32;">${JSON.stringify(data, null, 2)}</textarea>
        <div id="json-error" class="badge badge-error hidden" style="margin-top: 8px;">Invalid JSON format</div>
      </div>
    `;

    document.getElementById('json-editor-area').oninput = (e) => {
      try {
        JSON.parse(e.target.value);
        document.getElementById('json-error').classList.add('hidden');
      } catch (err) {
        document.getElementById('json-error').classList.remove('hidden');
      }
    };
  }
};

export default Components;
