# ./python/setup.py
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

from setuptools import setup, find_packages, Extension
from setuptools.command.build_ext import build_ext
from setuptools.command.build_py import build_py
from wheel.bdist_wheel import bdist_wheel


def compile_go_library():
    """компилирует Go-код и возвращает путь к готовому бинарному файлу"""
    python_dir = Path(__file__).parent.resolve()

    temp_dir = python_dir / 'build' / 'go'
    temp_dir.mkdir(parents=True, exist_ok=True)

    base_dir = python_dir.parent
    go_package_path = base_dir / 'cmd' / 'c-wrapper'

    system = platform.system()
    if system == 'Windows':
        lib_ext = '.pyd'
    elif system == 'Darwin':
        lib_ext = '.dylib'
    else:
        lib_ext = '.so'
    
    output_path = temp_dir / f'useragent{lib_ext}'

    target_arch = os.environ.get('TARGET_ARCH')
    if not target_arch:
        raise RuntimeError('переменная окружения TARGET_ARCH не установлена!')
    go_arch = {'x86_64': 'amd64', 'amd64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(target_arch.lower())
    if not go_arch:
        raise RuntimeError(f'неподдерживаемая архитектура: {target_arch}')

    print(f'[*] целевая архитектура: {target_arch} -> Go arch: {go_arch}')
    
    build_env = os.environ.copy()
    build_env['GOARCH'] = go_arch
    
    cmd = [
        'go', 'build', '-buildmode=c-shared', '-trimpath', '-buildvcs=false',
        '-ldflags', "-s -w", '-o', str(output_path), str(go_package_path)
    ]
    print(f"[*] вполнение команды: {' '.join(cmd)}")
    process = subprocess.run(cmd, check=False, capture_output=True, text=True, encoding='utf-8', env=build_env)
    if process.returncode != 0:
        print('[!] ошибка при сборке Go библиотеки:', file=sys.stderr)
        print('STDOUT:', process.stdout, file=sys.stderr)
        print('STDERR:', process.stderr, file=sys.stderr)
        raise subprocess.CalledProcessError(process.returncode, cmd)
    print(f'[+] библиотека успешно скомпилирована: {output_path}')
    return output_path


class CustomBuildExt(build_ext):
    """
    переопределение build_ext для компиляции Go и правильного размещения файлов
    """
    def run(self):
        print('[*] запуск CustomBuildExt для сборки Go')

        # 1. компилляция во временную папку
        compiled_lib_path = compile_go_library()

        # 2. путь куда setuptools ХОЧЕТ положить финальный файл
        ext = self.extensions[0]
        dest_dir = Path(self.get_ext_fullpath(ext.name)).parent

        # 3. переопределение пути куда задумано скопировать библиотеку
        dest_filename = f'useragent{compiled_lib_path.suffix}'
        dest_path = dest_dir / dest_filename

        dest_dir.mkdir(parents=True, exist_ok=True)

        print(f'[*] копирование скомпилированной библиотеки: {compiled_lib_path} -> {dest_path}')
        shutil.copyfile(compiled_lib_path, dest_path)

        # 4. копирование .h файла (вообще не обязательно)
        header_src = compiled_lib_path.with_suffix('.h')
        print(f'[*] копирование заголовочного файла: {header_src} -> {dest_dir}')
        shutil.copyfile(header_src, dest_dir / 'useragent.h')


class CustomBuildPy(build_py):
    def run(self):
        self.run_command('build_ext')
        super().run()


class CustomBdistWheel(bdist_wheel):
    def finalize_options(self):
        super().finalize_options()
        self.root_is_pure = False


useragent_ext = Extension(
    'py_fake_useragent.lib.useragent',
    sources=[]
)

setup(
    packages=find_packages(),
    ext_modules=[useragent_ext],
    zip_safe=False,
    cmdclass={
        'build_py': CustomBuildPy,
        'build_ext': CustomBuildExt,
        'bdist_wheel': CustomBdistWheel,
    },
)


