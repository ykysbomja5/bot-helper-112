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

    // закрываем мобильное меню если открыто
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
    // если вышли на десктоп, меню всегда открыто без нав-классов
    if (window.innerWidth > 768) {
      burger.classList.remove('is-open');
      nav.classList.remove('is-open');
    }
  });
}

// отправка формы 
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

  // Отправка формы
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    const data = new FormData(form);
    const name = data.get('name') || 'Гражданин';

    const district = data.get('district') || 'Не выбран';
    const category = data.get('category') || 'Не выбрана';

    // файлы
    const attachments = data.getAll('attachments');
    const files = attachments.filter((item) => item instanceof File && item.name);
    const filesInfo = files.length ? `\nПрикреплено файлов: ${files.length}.` : '';

    // геолокация
    const lat = data.get('latitude');
    const lng = data.get('longitude');
    const geoInfo = lat && lng ? `\nГеолокация: ${lat}, ${lng}.` : '';

    alert(
      `${name}, ваша заявка отправлена!` +
        `\nРайон: ${district}.` +
        `\nКатегория: ${category}.` +
        filesInfo +
        geoInfo +
        '\nМы свяжемся с вами в ближайшее время.'
    );

    form.reset();

    if (geoButton) {
      geoButton.textContent = defaultGeoLabel || '📍 Определить местоположение';
      geoButton.classList.remove('geo-button-success');
      geoButton.disabled = false;
    }
    if (geoDisplay) {
      geoDisplay.value = '';
    }
  });
}
