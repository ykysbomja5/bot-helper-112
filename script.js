// плавный скролл по клику на кнопки
const scrollButtons = document.querySelectorAll('[data-scroll]');
scrollButtons.forEach((btn) => {
  btn.addEventListener('click', () => {
    const target = btn.getAttribute('data-scroll');
    if (!target) return;
    const el = document.querySelector(target);
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const offset = window.pageYOffset || document.documentElement.scrollTop;
    const headerOffset = 72;
    const top = rect.top + offset - headerOffset;
    window.scrollTo({ top, behavior: 'smooth' });
    closeMobileNav();
  });
});

// бургер и мобильное меню
const burger = document.querySelector('.burger');
const nav = document.querySelector('.nav');

function closeMobileNav() {
  if (!burger || !nav) return;
  burger.classList.remove('is-open');
  nav.classList.remove('is-open');
}

if (burger && nav) {
  burger.addEventListener('click', () => {
    const isOpen = burger.classList.toggle('is-open');
    nav.classList.toggle('is-open', isOpen);
  });

  window.addEventListener('resize', () => {
    if (window.innerWidth > 768) {
      burger.classList.remove('is-open');
      nav.classList.remove('is-open');
    }
  });
}

const form = document.querySelector('.form');

if (form) {
  const geoButton = document.getElementById('geoButton');
  const geoDisplay = form.querySelector('.geo-display');
  const latInput = form.querySelector('input[name="latitude"]');
  const lngInput = form.querySelector('input[name="longitude"]');
  const defaultGeoLabel = geoButton ? (geoButton.dataset.defaultLabel || geoButton.textContent) : '';

  // геолокация
  if (geoButton && geoDisplay && latInput && lngInput) {
    if (!('geolocation' in navigator)) {
      geoButton.disabled = true;
      geoButton.textContent = 'Геолокация недоступна';
    } else {
      geoButton.addEventListener('click', () => {
        geoButton.disabled = true;
        const originalText = geoButton.textContent;
        geoButton.textContent = 'Определяем...';

        navigator.geolocation.getCurrentPosition(
          (pos) => {
            const { latitude, longitude } = pos.coords;
            const lat = latitude.toFixed(5);
            const lng = longitude.toFixed(5);

            geoDisplay.value = `${lat}, ${lng}`;
            latInput.value = latitude;
            lngInput.value = longitude;

            geoButton.textContent = 'Геолокация добавлена';
            geoButton.classList.add('geo-button-success');
            geoButton.disabled = false;
          },
          (err) => {
            alert('Не удалось получить геолокацию: ' + err.message);
            geoButton.textContent = originalText;
            geoButton.disabled = false;
          },
          {
            enableHighAccuracy: true,
            timeout: 10000,
          }
        );
      });
    }
  }

  form.addEventListener('submit', async (e) => {
    e.preventDefault();

    const data = new FormData(form);

    const name = (data.get('name') || 'Гражданин').toString().trim();
    const contact = (data.get('contact') || '').toString().trim();
    const district = (data.get('district') || '').toString();
    const category = (data.get('category') || '').toString();
    const description = (data.get('description') || '').toString().trim();
    const location = (data.get('location') || '').toString().trim() || null;

    const latRaw = data.get('latitude');
    const lngRaw = data.get('longitude');

    const latitude =
      latRaw && latRaw.toString().trim() !== '' ? parseFloat(latRaw.toString()) : null;
    const longitude =
      lngRaw && lngRaw.toString().trim() !== '' ? parseFloat(lngRaw.toString()) : null;

    // файлы
    const attachments = data.getAll('attachments');
    const files = attachments.filter((f) => f instanceof File && f.name);

    const payload = {
      name,
      contact,
      district,
      category,
      description,
      latitude,
      longitude,
      location,
    };

    const submitButton = form.querySelector('.submit-button');
    const submitLabel = form.querySelector('.submit-label');

    try {
      if (submitButton && submitLabel) {
        submitButton.disabled = true;
        submitLabel.textContent = 'Отправляем...';
      }

      const resIssue = await fetch('/api/issues', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!resIssue.ok) {
        const errJson = await resIssue.json().catch(() => ({}));
        throw new Error(errJson.error || 'Ошибка при создании заявки');
      }

      const issue = await resIssue.json();
      const issueId = issue.id;

      if (files.length && issueId) {
        const fd = new FormData();
        files.forEach((file) => fd.append('attachments', file));

        const resFiles = await fetch(`/api/issues/${issueId}/attachments`, {
          method: 'POST',
          body: fd,
        });

        if (!resFiles.ok) {
          console.error('Ошибка загрузки файлов', await resFiles.text());
        }
      }

      alert(
        `${name}, ваша заявка отправлена!\n` +
          `ID: ${issueId}\n` +
          `Район: ${district || 'не указан'}\n` +
          `Категория: ${category || 'не указана'}\n` +
          (files.length ? `Прикреплено файлов: ${files.length}\n` : '') +
          '\nМы свяжемся с вами в ближайшее время.'
      );

      form.reset();

      if (geoButton) {
        geoButton.textContent = defaultGeoLabel || '📍 Определить местоположение';
        geoButton.classList.remove('geo-button-success');
        geoButton.disabled = false;
      }
      if (geoDisplay) geoDisplay.value = '';
    } catch (err) {
      console.error('Ошибка отправки заявки:', err);
      alert('Не удалось отправить заявку: ' + err.message);
    } finally {
      if (submitButton && submitLabel) {
        submitButton.disabled = false;
        submitLabel.textContent = 'Отправить заявку';
      }
    }
  });
}
