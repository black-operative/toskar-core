import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { GPUSetupList } from './GPUSetupList'

describe('GPUSetupList', () => {
  it('shows each missing piece with its command and a copy button', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    render(
      <GPUSetupList
        problems={[
          { code: 'vulkan_loader_missing', command: 'sudo apt-get install libvulkan1 mesa-vulkan-drivers vulkan-tools' },
          { code: 'nvidia_driver_missing', command: 'sudo dnf install akmod-nvidia', url: 'https://rpmfusion.org/Howto/NVIDIA' },
          { code: 'cpu_build' },
        ]}
      />,
    )
    expect(screen.getByText(/Vulkan isn't installed/)).toBeInTheDocument()
    expect(screen.getByText('sudo apt-get install libvulkan1 mesa-vulkan-drivers vulkan-tools')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Download the driver' })).toHaveAttribute('href', 'https://rpmfusion.org/Howto/NVIDIA')
    expect(screen.getByText(/runs on the CPU only/)).toBeInTheDocument()
    fireEvent.click(screen.getAllByRole('button', { name: 'Copy' })[1])
    expect(writeText).toHaveBeenCalledWith('sudo dnf install akmod-nvidia')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument())
  })
})
